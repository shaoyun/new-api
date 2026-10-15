package service

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAttachQuotaSaturationNestsUnderAdminInfo verifies the saturation marker
// is nested under other.admin_info.quota_saturation so it is admin-only (the
// log formatter strips admin_info for non-admin viewers).
func TestAttachQuotaSaturationNestsUnderAdminInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)

	relayInfo := &relaycommon.RelayInfo{
		UserId:          7,
		OriginModelName: "gpt-image-1",
		QuotaClamp: &common.QuotaClamp{
			Op:       "QuotaFromDecimal",
			Kind:     common.QuotaClampOverflow,
			Original: 1.8e19,
			Clamped:  common.MaxQuota,
		},
	}

	other := model.NewLogOther()
	other.SetPublic("model_price", 0.004)
	attachQuotaSaturation(ctx, relayInfo, other)

	adminInfo, ok := other.Snapshot()["admin_info"].(map[string]any)
	require.True(t, ok, "admin_info should be created")
	sat, ok := adminInfo["quota_saturation"].(map[string]any)
	require.True(t, ok, "quota_saturation should be nested under admin_info")
	require.Equal(t, "QuotaFromDecimal", sat["op"])
	require.Equal(t, common.QuotaClampOverflow, sat["kind"])
	require.Equal(t, common.MaxQuota, sat["clamped"])
}

func TestCalcViolationFeeQuotaSaturates(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	require.Equal(t, common.MaxQuota, calcViolationFeeQuota(1e20, 1))
}

// TestChargeViolationFeeLogsBaseGroupRatioAndDiscount verifies the violation-fee
// consume log follows the same contract as other writers: group_ratio is the
// true base ratio and group_model_discount is written only on a rule hit, while
// the fee itself stays computed from the charged (discounted) ratio.
func TestChargeViolationFeeLogsBaseGroupRatioAndDiscount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
	settings := model_setting.GetGrokSettings()
	oldSettings := *settings
	settings.ViolationDeductionEnabled = true
	settings.ViolationDeductionAmount = 0.05
	t.Cleanup(func() { *settings = oldSettings })

	tests := []struct {
		name         string
		info         hosttypes.GroupRatioInfo
		wantGroup    float64
		wantDiscount any
		wantFee      int
	}{
		{name: "rule hit", info: hosttypes.GroupRatioInfo{GroupRatio: 0.8, BaseGroupRatio: 1, GroupModelDiscount: 0.8}, wantGroup: 1, wantDiscount: 0.8, wantFee: 20000},
		{name: "no rule", info: hosttypes.GroupRatioInfo{GroupRatio: 1.5, BaseGroupRatio: 1.5, GroupModelDiscount: 1}, wantGroup: 1.5, wantFee: 37500},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			userID := 60 + i
			seedUser(t, userID, 1_000_000)
			seedChannel(t, userID)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			relayInfo := &relaycommon.RelayInfo{
				UserId:          userID,
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: userID},
				OriginModelName: "grok-test",
				UsingGroup:      "default",
				StartTime:       time.Now(),
				PriceData:       hosttypes.PriceData{GroupRatioInfo: tc.info},
			}
			apiErr := types.NewErrorWithStatusCode(errors.New("violation"), types.ErrorCodeViolationFeeGrokCSAM, http.StatusBadRequest)

			require.True(t, ChargeViolationFeeIfNeeded(ctx, relayInfo, apiErr))

			log := getLastLog(t)
			require.NotNil(t, log)
			assert.Equal(t, tc.wantFee, log.Quota)
			assert.Equal(t, 1_000_000-tc.wantFee, getUserQuota(t, userID))
			other := map[string]any{}
			require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
			assert.Equal(t, true, other["violation_fee"])
			assert.Equal(t, tc.wantGroup, other["group_ratio"])
			if tc.wantDiscount != nil {
				assert.Equal(t, tc.wantDiscount, other["group_model_discount"])
			} else {
				assert.NotContains(t, other, "group_model_discount")
			}
		})
	}
}

func TestCalcOpenRouterCacheCreateTokensDoesNotWrap(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	got := CalcOpenRouterCacheCreateTokens(dto.Usage{Cost: math.Inf(1)}, hosttypes.PriceData{
		ModelRatio:         1,
		CacheCreationRatio: 2,
		CacheRatio:         1,
		CompletionRatio:    1,
	})
	require.Equal(t, -1, got)
}

// TestAttachQuotaSaturationPreservesExistingAdminInfo verifies the marker is
// merged into a pre-existing admin_info map without clobbering it.
func TestAttachQuotaSaturationPreservesExistingAdminInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)

	relayInfo := &relaycommon.RelayInfo{
		QuotaClamp: &common.QuotaClamp{Op: "QuotaFromFloat", Kind: common.QuotaClampUnderflow, Clamped: common.MinQuota},
	}
	other := model.NewLogOther()
	other.SetAdmin("admin_username", "root")
	attachQuotaSaturation(ctx, relayInfo, other)

	adminInfo := other.Snapshot()["admin_info"].(map[string]any)
	require.Equal(t, "root", adminInfo["admin_username"], "existing admin_info fields preserved")
	require.NotNil(t, adminInfo["quota_saturation"])
}

// TestAttachQuotaSaturationNoClampNoMarker verifies the common case (no
// saturation) leaves the log untouched.
func TestAttachQuotaSaturationNoClampNoMarker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)

	relayInfo := &relaycommon.RelayInfo{QuotaClamp: nil}
	other := model.NewLogOther()
	other.SetPublic("model_price", 0.004)
	attachQuotaSaturation(ctx, relayInfo, other)

	_, hasAdmin := other.Snapshot()["admin_info"]
	require.False(t, hasAdmin, "no admin_info should be added when there is no clamp")
}

func TestPreConsumeBillingRejectsSaturatedQuotaBeforeDeduction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		QuotaClamp: &common.QuotaClamp{
			Op:       "QuotaFromFloat",
			Kind:     common.QuotaClampOverflow,
			Original: 1e30,
			Clamped:  common.MaxQuota,
		},
	}

	apiErr := PreConsumeBilling(c, common.MaxQuota, info)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeModelPriceError, apiErr.GetErrorCode())
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Same(t, info.QuotaClamp, apiErr.Err)
	var clamp *common.QuotaClamp
	require.ErrorAs(t, apiErr, &clamp)
	require.Same(t, info.QuotaClamp, clamp)
	require.Nil(t, info.Billing)
}

func TestPreConsumeBillingRejectsNegativeQuotaBeforeDeduction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{}

	apiErr := PreConsumeBilling(c, -1, info)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeModelPriceError, apiErr.GetErrorCode())
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Nil(t, info.Billing)
}
