package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSumUsedQuotaCacheAndSuccessStats 保护统计接口的两条契约:
//  1. 缓存命中率所需的 prompt_tokens/cache_tokens 聚合——cache_tokens 从 other JSON 文本提取,
//     且 other 为空串或非法 JSON 的历史脏行不会让整条聚合报错(守卫降级),只贡献 0。
//  2. 成功率所需的成功(type=2)/失败(type=5)计数——跨两类日志统计,且错误日志不污染
//     仅消费口径的 prompt/cache/quota 汇总。
func TestSumUsedQuotaCacheAndSuccessStats(t *testing.T) {
	require.True(t, common.UsingLogDatabase(common.DatabaseTypeSQLite), "test relies on SQLite json_extract path")
	require.True(t, common.LogConsumeEnabled, "success rate query requires consume logging enabled")

	const user = "stat_cache_success_user"
	var base int64 = 1_700_000_000

	// 隔离:清掉该用户历史行,避免其它测试数据干扰断言。
	require.NoError(t, LOG_DB.Where("username = ?", user).Delete(&Log{}).Error)

	rows := []*Log{
		// 消费日志:prompt/cache/quota 参与汇总。
		{UserId: 1, Username: user, CreatedAt: base, Type: LogTypeConsume, PromptTokens: 100, Quota: 10, Other: `{"cache_tokens":40}`},
		{UserId: 1, Username: user, CreatedAt: base, Type: LogTypeConsume, PromptTokens: 200, Quota: 20, Other: `{"cache_tokens":60}`},
		// 边界:消费日志 other 为空串,守卫应使 cache 记 0,prompt 仍计入。
		{UserId: 1, Username: user, CreatedAt: base, Type: LogTypeConsume, PromptTokens: 50, Quota: 5, Other: ``},
		// 边界:消费日志 other 为非法(非空)JSON,守卫应使 cache 记 0 且绝不让整条聚合报错。
		{UserId: 1, Username: user, CreatedAt: base, Type: LogTypeConsume, PromptTokens: 70, Quota: 7, Other: `{bad json`},
		// 错误日志:计入失败计数,但不进入 prompt/cache/quota 汇总。
		{UserId: 1, Username: user, CreatedAt: base, Type: LogTypeError, PromptTokens: 999, Quota: 999, Other: `{"cache_tokens":999}`},
		{UserId: 1, Username: user, CreatedAt: base, Type: LogTypeError},
	}
	for _, r := range rows {
		require.NoError(t, createLog(r))
	}

	stat, err := SumUsedQuota(0, base-10, base+10, "", user, "", 0, "")
	require.NoError(t, err)

	// 仅消费日志的汇总:prompt 100+200+50+70=420,cache 40+60+0+0=100,quota 10+20+5+7=42。
	assert.Equal(t, 420, stat.PromptTokens, "prompt tokens sum over consume logs")
	assert.Equal(t, 100, stat.CacheTokens, "cache tokens sum ignores empty/invalid other")
	assert.Equal(t, 42, stat.Quota, "quota sum excludes error logs")

	// 成功率计数跨消费与错误两类。
	assert.Equal(t, 4, stat.SuccessCount, "success = consume log count")
	assert.Equal(t, 2, stat.ErrorCount, "error = error log count")
}

// TestSumUsedQuotaSuccessSkippedWhenConsumeLogDisabled 保护契约:LogConsumeEnabled 关闭时,
// 消费日志不落库而错误日志仍落库,成功率会失真为 0%。此时应跳过成功率查询,让计数保持 0,
// 由前端渲染为 “-”(不可用)。用量/缓存查询不受此开关影响,仍照常统计。
func TestSumUsedQuotaSuccessSkippedWhenConsumeLogDisabled(t *testing.T) {
	const user = "stat_consume_disabled_user"
	var base int64 = 1_700_000_100

	require.NoError(t, LOG_DB.Where("username = ?", user).Delete(&Log{}).Error)

	rows := []*Log{
		{UserId: 2, Username: user, CreatedAt: base, Type: LogTypeConsume, PromptTokens: 100, Quota: 10, Other: `{"cache_tokens":25}`},
		{UserId: 2, Username: user, CreatedAt: base, Type: LogTypeError},
		{UserId: 2, Username: user, CreatedAt: base, Type: LogTypeError},
	}
	for _, r := range rows {
		require.NoError(t, createLog(r))
	}

	original := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	defer func() { common.LogConsumeEnabled = original }()

	stat, err := SumUsedQuota(0, base-10, base+10, "", user, "", 0, "")
	require.NoError(t, err)

	// 成功率查询被跳过,计数保持 0(前端据此显示 “-”),不产生误导性的 0%。
	assert.Equal(t, 0, stat.SuccessCount, "success count must stay 0 when consume logging disabled")
	assert.Equal(t, 0, stat.ErrorCount, "error count must stay 0 when consume logging disabled")
	// 用量/缓存仍照常统计,不受开关影响。
	assert.Equal(t, 100, stat.PromptTokens, "usage query unaffected by consume-log toggle")
	assert.Equal(t, 25, stat.CacheTokens, "cache query unaffected by consume-log toggle")
}
