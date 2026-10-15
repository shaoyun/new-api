package model

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserDiscountRuleUpsertLookupDelete(t *testing.T) {
	setupUserUpdateTestState(t)
	require.NoError(t, UpsertUserDiscountRules(7, "vip", []string{"m-a", "m-b"}, 0.8))
	require.NoError(t, UpsertUserDiscountRules(7, "vip", []string{"m-b"}, 0.9)) // 覆盖
	require.NoError(t, UpsertUserDiscountRules(8, "vip", []string{"m-a"}, 0.5))

	assert.Equal(t, 0.8, GetUserGroupModelDiscount(7, "vip", "m-a"))
	assert.Equal(t, 0.9, GetUserGroupModelDiscount(7, "vip", "m-b"))
	assert.Equal(t, 1.0, GetUserGroupModelDiscount(7, "default", "m-a")) // 分组不同
	assert.Equal(t, 1.0, GetUserGroupModelDiscount(7, "vip", "m-c"))     // 模型不同
	assert.Equal(t, 1.0, GetUserGroupModelDiscount(9, "vip", "m-a"))     // 无规则用户
	assert.Equal(t, 1.0, GetUserGroupModelDiscount(0, "vip", "m-a"))     // 无用户（未登录/测试渠道）

	rules, err := ListUserDiscountRules(7)
	require.NoError(t, err)
	require.Len(t, rules, 2)
	assert.Equal(t, "m-a", rules[0].Model)
	assert.Equal(t, "m-b", rules[1].Model)

	ruleMap, err := GetUserDiscountRuleMap(7)
	require.NoError(t, err)
	assert.Equal(t, map[string]map[string]float64{"vip": {"m-a": 0.8, "m-b": 0.9}}, ruleMap)

	counts, err := CountUserDiscountRules([]int{7, 8, 9})
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{7: 2, 8: 1}, counts)

	other, err := ListUserDiscountRules(8)
	require.NoError(t, err)
	require.Len(t, other, 1)
	n, err := DeleteUserDiscountRules(7, []int{rules[0].Id, other[0].Id}) // 跨用户 id 被忽略
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	assert.Equal(t, 0.5, GetUserGroupModelDiscount(8, "vip", "m-a"))
	assert.Equal(t, 1.0, GetUserGroupModelDiscount(7, "vip", "m-a"))
}

func TestUpsertUserDiscountRulesRejectsInvalidInput(t *testing.T) {
	setupUserUpdateTestState(t)
	for _, discount := range []float64{0, 1, 1.5, -0.2, math.NaN()} {
		assert.Error(t, UpsertUserDiscountRules(7, "vip", []string{"m-a"}, discount))
	}
	assert.NoError(t, UpsertUserDiscountRules(7, "vip", nil, 0.5))
	counts, err := CountUserDiscountRules([]int{7})
	require.NoError(t, err)
	assert.Empty(t, counts)
}

func TestGetUserGroupModelDiscountFallsBackOnError(t *testing.T) {
	setupUserUpdateTestState(t)
	require.NoError(t, DB.Migrator().DropTable(&UserDiscountRule{}))
	t.Cleanup(func() { require.NoError(t, DB.AutoMigrate(&UserDiscountRule{})) })
	assert.Equal(t, 1.0, GetUserGroupModelDiscount(7, "vip", "m-a"))
}

func TestGetUserGroupModelDiscountIgnoresInvalidStoredValues(t *testing.T) {
	setupUserUpdateTestState(t)
	for _, tc := range []struct {
		model    string
		discount float64
	}{
		{"too-high", 1.5},
		{"zero", 0},
		{"one", 1},
		{"negative", -0.5},
	} {
		require.NoError(t, DB.Create(&UserDiscountRule{UserId: 7, Group: "vip", Model: tc.model, Discount: tc.discount}).Error)
		assert.Equal(t, 1.0, GetUserGroupModelDiscount(7, "vip", tc.model), tc.model)
	}
}

func TestUserDiscountRuleCache(t *testing.T) {
	setupUserUpdateTestState(t)
	mr := miniredis.RunT(t)
	oldRDB := common.RDB
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	common.RedisEnabled = true // setupUserUpdateTestState 的 Cleanup 会还原
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB = oldRDB
	})

	assert.Equal(t, 1.0, GetUserGroupModelDiscount(9, "vip", "m-a"))
	require.True(t, mr.Exists("user_discount_rules:9"))
	cached, err := mr.Get("user_discount_rules:9")
	require.NoError(t, err)
	assert.Equal(t, "{}", cached)

	require.NoError(t, UpsertUserDiscountRules(7, "vip", []string{"m-a"}, 0.8))
	assert.Equal(t, 0.8, GetUserGroupModelDiscount(7, "vip", "m-a"))
	require.True(t, mr.Exists("user_discount_rules:7"))

	require.NoError(t, UpsertUserDiscountRules(7, "vip", []string{"m-a"}, 0.6))
	assert.False(t, mr.Exists("user_discount_rules:7"))
	assert.Equal(t, 0.6, GetUserGroupModelDiscount(7, "vip", "m-a"))

	rules, err := ListUserDiscountRules(7)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	n, err := DeleteUserDiscountRules(7, []int{rules[0].Id})
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	assert.False(t, mr.Exists("user_discount_rules:7"))
	assert.Equal(t, 1.0, GetUserGroupModelDiscount(7, "vip", "m-a"))

	// 缓存损坏时回源 DB
	require.NoError(t, UpsertUserDiscountRules(7, "vip", []string{"m-a"}, 0.7))
	require.NoError(t, mr.Set("user_discount_rules:7", "not-json"))
	assert.Equal(t, 0.7, GetUserGroupModelDiscount(7, "vip", "m-a"))
}

func TestMigrateLegacyUserDiscounts(t *testing.T) {
	setupUserUpdateTestState(t)
	for _, ab := range []Ability{
		{Group: "vip", Model: "m-a", ChannelId: 1, Enabled: true},
		{Group: "vip", Model: "m-a", ChannelId: 2, Enabled: true}, // 同模型多渠道，只展开一次
		{Group: "vip", Model: "m-b", ChannelId: 1, Enabled: true},
		{Group: "vip", Model: "m-off", ChannelId: 1, Enabled: false},
		{Group: "default", Model: "m-a", ChannelId: 1, Enabled: true},
	} {
		require.NoError(t, DB.Create(&ab).Error)
	}
	newUser := func(name string, discount float64) User {
		user := User{
			Username: name,
			Password: "unused-password-hash",
			Role:     common.RoleCommonUser,
			Status:   common.UserStatusEnabled,
			Group:    "default",
			AffCode:  name + "-aff",
			Discount: discount,
		}
		require.NoError(t, DB.Create(&user).Error)
		return user
	}
	userA := newUser("legacy-a", 0.8)
	userB := newUser("legacy-b", 0)
	userC := newUser("legacy-c", 1.5)
	require.NoError(t, UpsertUserDiscountRules(userA.Id, "vip", []string{"m-a"}, 0.9))

	usableGroups := func(string) []string { return []string{"default", "vip", "auto"} }
	require.NoError(t, MigrateLegacyUserDiscounts(usableGroups))

	ruleMap, err := GetUserDiscountRuleMap(userA.Id)
	require.NoError(t, err)
	assert.Equal(t, map[string]map[string]float64{
		"default": {"m-a": 0.8},
		"vip":     {"m-a": 0.9, "m-b": 0.8}, // 已有规则不被覆盖，禁用模型不展开
	}, ruleMap)

	var reloadedA, reloadedC User
	require.NoError(t, DB.First(&reloadedA, userA.Id).Error)
	assert.Equal(t, 0.0, reloadedA.Discount)
	require.NoError(t, DB.First(&reloadedC, userC.Id).Error)
	assert.Equal(t, 1.5, reloadedC.Discount)

	counts, err := CountUserDiscountRules([]int{userA.Id, userB.Id, userC.Id})
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{userA.Id: 3}, counts)

	require.NoError(t, MigrateLegacyUserDiscounts(usableGroups))
	counts, err = CountUserDiscountRules([]int{userA.Id, userB.Id, userC.Id})
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{userA.Id: 3}, counts)
}
