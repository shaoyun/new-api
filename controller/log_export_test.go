package controller

import (
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shanghai is the export's bucketing timezone; the day boundary is what decides
// which line a request lands on.
var shanghai = time.FixedZone("CST", 8*3600)

// at builds a log timestamp from a Shanghai-local wall clock time.
func at(year int, month time.Month, day, hour, min int) int64 {
	return time.Date(year, month, day, hour, min, 0, 0, shanghai).Unix()
}

func TestParseUsernames(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty", raw: "", want: nil},
		{name: "blanks only", raw: " , , ", want: nil},
		{name: "single", raw: "alice", want: []string{"alice"}},
		{name: "trim and split", raw: " alice , bob ", want: []string{"alice", "bob"}},
		{name: "dedup preserves order", raw: "bob,alice,bob", want: []string{"bob", "alice"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseUsernames(tt.raw))
		})
	}
}

func TestFormatQuotaUSD(t *testing.T) {
	// QuotaPerUnit is process-global and runtime-overridable; pin it for the test.
	orig := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	defer func() { common.QuotaPerUnit = orig }()

	tests := []struct {
		name  string
		quota int64
		want  string
	}{
		{name: "zero", quota: 0, want: "0"},
		{name: "trims trailing zeros", quota: 220, want: "0.00044"},
		{name: "whole dollar", quota: 500000, want: "1"},
		{name: "small fraction", quota: 15, want: "0.00003"},
		// A day of billing can sum past the int32 range of the quota column.
		{name: "beyond int32", quota: 3_000_000_000, want: "6000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatQuotaUSD(tt.quota))
		})
	}
}

func TestFoldBillingExportBatchSumsPerDayUserTokenModel(t *testing.T) {
	logs := []*model.Log{
		// Same day/user/token/model, three requests -> one summed line.
		{CreatedAt: at(2026, 8, 24, 0, 11), Username: "xiaoshi", TokenName: "gpt", ModelName: "gpt-5.6-luna",
			PromptTokens: 7595, CompletionTokens: 250, Quota: 4548, Other: `{"cache_tokens":0,"cache_creation_tokens":0}`},
		{CreatedAt: at(2026, 8, 24, 2, 42), Username: "xiaoshi", TokenName: "gpt", ModelName: "gpt-5.6-luna",
			PromptTokens: 60263, CompletionTokens: 8487, Quota: 55593, Other: `{"cache_tokens":100,"cache_creation_tokens":7}`},
		{CreatedAt: at(2026, 8, 24, 23, 59), Username: "xiaoshi", TokenName: "gpt", ModelName: "gpt-5.6-luna",
			PromptTokens: 5, CompletionTokens: 1, Quota: 2, Other: ``},
		// Different model on the same day -> separate line.
		{CreatedAt: at(2026, 8, 24, 2, 52), Username: "xiaoshi", TokenName: "gpt", ModelName: "gpt-5.6-sol",
			PromptTokens: 40348, CompletionTokens: 301, Quota: 105385, Other: `{"cache_tokens":40345}`},
		// Same model, next day -> separate line.
		{CreatedAt: at(2026, 8, 25, 0, 5), Username: "xiaoshi", TokenName: "gpt", ModelName: "gpt-5.6-luna",
			PromptTokens: 10, CompletionTokens: 2, Quota: 6, Other: ``},
		// Same day/model, different user and token -> separate lines.
		{CreatedAt: at(2026, 8, 24, 9, 0), Username: "alice", TokenName: "gpt", ModelName: "gpt-5.6-luna",
			PromptTokens: 20, CompletionTokens: 3, Quota: 9, Other: ``},
		{CreatedAt: at(2026, 8, 24, 9, 0), Username: "xiaoshi", TokenName: "backup", ModelName: "gpt-5.6-luna",
			PromptTokens: 30, CompletionTokens: 4, Quota: 12, Other: ``},
	}

	buckets := make(map[billingExportKey]*billingExportRow)
	foldBillingExportBatch(buckets, logs, shanghai)
	rows := takeCompletedDays(buckets, math.MaxInt64)

	want := []billingExportRow{
		{billingExportKey: billingExportKey{at(2026, 8, 24, 0, 0), "alice", "gpt", "gpt-5.6-luna"},
			PromptTokens: 20, CompletionTokens: 3, Quota: 9},
		{billingExportKey: billingExportKey{at(2026, 8, 24, 0, 0), "xiaoshi", "backup", "gpt-5.6-luna"},
			PromptTokens: 30, CompletionTokens: 4, Quota: 12},
		{billingExportKey: billingExportKey{at(2026, 8, 24, 0, 0), "xiaoshi", "gpt", "gpt-5.6-luna"},
			PromptTokens: 67863, CompletionTokens: 8738, CacheReadTokens: 100, CacheWriteTokens: 7, Quota: 60143},
		{billingExportKey: billingExportKey{at(2026, 8, 24, 0, 0), "xiaoshi", "gpt", "gpt-5.6-sol"},
			PromptTokens: 40348, CompletionTokens: 301, CacheReadTokens: 40345, Quota: 105385},
		{billingExportKey: billingExportKey{at(2026, 8, 25, 0, 0), "xiaoshi", "gpt", "gpt-5.6-luna"},
			PromptTokens: 10, CompletionTokens: 2, Quota: 6},
	}

	require.Len(t, rows, len(want))
	for i := range want {
		assert.Equal(t, want[i], *rows[i])
	}
}

func TestFoldBillingExportBatchDayBoundaryIsShanghaiLocal(t *testing.T) {
	// 2026-08-23 16:30 UTC is 2026-08-24 00:30 in Shanghai, so it must bucket
	// into the 24th — bucketing on the raw UTC date would put it on the 23rd.
	logs := []*model.Log{
		{CreatedAt: at(2026, 8, 24, 0, 30), Username: "u", TokenName: "t", ModelName: "m", Quota: 1},
		// 23:30 the previous Shanghai day stays on the 23rd.
		{CreatedAt: at(2026, 8, 23, 23, 30), Username: "u", TokenName: "t", ModelName: "m", Quota: 2},
	}

	buckets := make(map[billingExportKey]*billingExportRow)
	foldBillingExportBatch(buckets, logs, shanghai)
	rows := takeCompletedDays(buckets, math.MaxInt64)

	require.Len(t, rows, 2)
	assert.Equal(t, at(2026, 8, 23, 0, 0), rows[0].DayStart)
	assert.Equal(t, int64(2), rows[0].Quota)
	assert.Equal(t, at(2026, 8, 24, 0, 0), rows[1].DayStart)
	assert.Equal(t, int64(1), rows[1].Quota)
}

func TestFoldBillingExportBatchAccumulatesAcrossBatches(t *testing.T) {
	// The export folds one keyset page at a time into a shared map; a second
	// page touching the same bucket must add to it, not replace it.
	buckets := make(map[billingExportKey]*billingExportRow)
	entry := func(quota int) []*model.Log {
		return []*model.Log{{
			CreatedAt: at(2026, 8, 24, 1, 0), Username: "u", TokenName: "t", ModelName: "m",
			PromptTokens: 5, Quota: quota, Other: `{"cache_tokens":3}`,
		}}
	}

	foldBillingExportBatch(buckets, entry(10), shanghai)
	foldBillingExportBatch(buckets, entry(20), shanghai)

	rows := takeCompletedDays(buckets, math.MaxInt64)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(30), rows[0].Quota)
	assert.Equal(t, int64(10), rows[0].PromptTokens)
	assert.Equal(t, int64(6), rows[0].CacheReadTokens)
}

func TestTakeCompletedDaysHoldsBackTheDayStillBeingScanned(t *testing.T) {
	// The export writes each day out as soon as the scan crosses into the next
	// one. A day the scan is still inside must stay in the map, or later rows
	// for it would be emitted as a second line for the same day.
	buckets := make(map[billingExportKey]*billingExportRow)
	foldBillingExportBatch(buckets, []*model.Log{
		{CreatedAt: at(2026, 8, 23, 9, 0), Username: "u", TokenName: "t", ModelName: "m", Quota: 1},
		{CreatedAt: at(2026, 8, 24, 9, 0), Username: "u", TokenName: "t", ModelName: "m", Quota: 2},
	}, shanghai)

	// Scan has reached the 24th, so only the 23rd is final.
	rows := takeCompletedDays(buckets, at(2026, 8, 24, 0, 0))
	require.Len(t, rows, 1)
	assert.Equal(t, at(2026, 8, 23, 0, 0), rows[0].DayStart)
	assert.Equal(t, int64(1), rows[0].Quota)
	require.Len(t, buckets, 1)

	// A later batch still lands in the held-back day rather than a new line.
	foldBillingExportBatch(buckets, []*model.Log{
		{CreatedAt: at(2026, 8, 24, 22, 0), Username: "u", TokenName: "t", ModelName: "m", Quota: 3},
	}, shanghai)

	rows = takeCompletedDays(buckets, math.MaxInt64)
	require.Len(t, rows, 1)
	assert.Equal(t, at(2026, 8, 24, 0, 0), rows[0].DayStart)
	assert.Equal(t, int64(5), rows[0].Quota)
	assert.Empty(t, buckets)
}

func TestOtherIntFromDecodedJSON(t *testing.T) {
	// `other` is stored as JSON text; decoding yields float64 for numbers.
	other, err := common.StrToMap(`{"cache_tokens":128,"cache_creation_tokens":64}`)
	require.NoError(t, err)

	assert.Equal(t, 128, otherInt(other, "cache_tokens"))
	assert.Equal(t, 64, otherInt(other, "cache_creation_tokens"))
	assert.Equal(t, 0, otherInt(other, "missing_key"))
	assert.Equal(t, 0, otherInt(nil, "cache_tokens"))
}
