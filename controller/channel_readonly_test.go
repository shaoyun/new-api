package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
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
