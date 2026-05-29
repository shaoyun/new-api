package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newChannelWithSecrets() *model.Channel {
	settingJSON := `{"proxy":"http://user:pass@127.0.0.1:8080","force_format":true}`
	ch := &model.Channel{
		Id:      1,
		Name:    "secret-channel",
		Key:     "sk-super-secret",
		Setting: &settingJSON,
	}
	return ch
}

func TestSanitizeChannelForRole_CommonUserStripsKeyAndProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("role", common.RoleCommonUser)

	ch := newChannelWithSecrets()
	sanitizeChannelForRole(c, ch)

	require.Equal(t, "", ch.Key, "common user must not see key")
	require.Equal(t, "", ch.GetSetting().Proxy, "common user must not see proxy")
	require.True(t, ch.GetSetting().ForceFormat, "non-secret setting fields must be preserved")
}

func TestSanitizeChannelForRole_AdminKeepsEverything(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("role", common.RoleAdminUser)

	ch := newChannelWithSecrets()
	sanitizeChannelForRole(c, ch)

	require.Equal(t, "sk-super-secret", ch.Key, "admin must keep key")
	require.Equal(t, "http://user:pass@127.0.0.1:8080", ch.GetSetting().Proxy, "admin must keep proxy")
}

func TestSanitizeChannelForRole_RootKeepsEverything(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("role", common.RoleRootUser)

	ch := newChannelWithSecrets()
	sanitizeChannelForRole(c, ch)

	require.Equal(t, "sk-super-secret", ch.Key, "root must keep key")
	require.Equal(t, "http://user:pass@127.0.0.1:8080", ch.GetSetting().Proxy, "root must keep proxy")
}

func TestSanitizeChannelForRole_NilChannelNoPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("role", common.RoleCommonUser)
	require.NotPanics(t, func() { sanitizeChannelForRole(c, nil) })
}

func setupChannelReadonlyDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.Channel{}))

	settingJSON := `{"proxy":"http://secret-proxy:8080"}`
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "ch1", Key: "sk-secret", Setting: &settingJSON,
	}).Error)
}

func doGetChannel(role int) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("role", role)
	c.Params = gin.Params{{Key: "id", Value: "1"}}
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/channel/1", nil)
	GetChannel(c)
	return w
}

func TestGetChannel_CommonUserHidesSecrets(t *testing.T) {
	setupChannelReadonlyDB(t)
	w := doGetChannel(common.RoleCommonUser)

	var resp struct {
		Success bool          `json:"success"`
		Data    model.Channel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Equal(t, "", resp.Data.Key)
	require.Equal(t, "", resp.Data.GetSetting().Proxy)
}

func TestGetChannel_AdminSeesProxyKeyOmittedByDB(t *testing.T) {
	setupChannelReadonlyDB(t)
	w := doGetChannel(common.RoleAdminUser)

	var resp struct {
		Success bool          `json:"success"`
		Data    model.Channel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	// GetChannel uses GetChannelById(id,false) → DB Omit("key"), so admin also won't get key here (same as before this change)
	require.Equal(t, "", resp.Data.Key)
	// proxy must be preserved for admin
	require.Equal(t, "http://secret-proxy:8080", resp.Data.GetSetting().Proxy)
}
