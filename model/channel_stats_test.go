package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetChannelStats(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&ChannelStat{}))
	channelStatsMu.Lock()
	channelStatsPending = map[channelStatKey]*ChannelStatsSummary{}
	channelStatsMu.Unlock()
	require.NoError(t, DB.Exec("DELETE FROM channel_stats").Error)
	t.Cleanup(func() { DB.Exec("DELETE FROM channel_stats") })
}

func TestChannelStatsFlushAccumulates(t *testing.T) {
	resetChannelStats(t)

	AddChannelStat(7, 1, 0, 100, 40)
	require.NoError(t, FlushChannelStats())
	AddChannelStat(7, 2, 1, 50, 10)
	require.NoError(t, FlushChannelStats())

	got, err := GetChannelStatsSummaries([]int{7}, 0)
	require.NoError(t, err)
	require.Contains(t, got, 7)
	assert.Equal(t, ChannelStatsSummary{Success: 3, Fail: 1, InputTokens: 150, CacheTokens: 50}, *got[7])

	var rows int64
	require.NoError(t, DB.Model(&ChannelStat{}).Count(&rows).Error)
	assert.Equal(t, int64(1), rows)
}

func TestChannelStatsWindow(t *testing.T) {
	resetChannelStats(t)

	current := common.GetTimestamp() / 3600 * 3600
	since := current - 23*3600
	require.NoError(t, DB.Create(&[]ChannelStat{
		{ChannelId: 9, BucketTs: since - 3600, SuccessCount: 100},
		{ChannelId: 9, BucketTs: since, SuccessCount: 2, FailCount: 1},
		{ChannelId: 9, BucketTs: current, SuccessCount: 3},
	}).Error)
	// Not flushed yet: must not show up in the summary.
	AddChannelStat(9, 50, 0, 0, 0)

	got, err := GetChannelStatsSummaries([]int{9, 10}, since)
	require.NoError(t, err)
	require.Contains(t, got, 9)
	assert.Equal(t, ChannelStatsSummary{Success: 5, Fail: 1}, *got[9])
	assert.NotContains(t, got, 10)
}

func TestChannelStatInputTokens(t *testing.T) {
	cases := []struct {
		name      string
		prompt    int
		other     map[string]any
		wantInput int64
		wantCache int64
	}{
		{"openai prompt includes cache", 100, map[string]any{"cache_tokens": 40}, 100, 40},
		{"anthropic adds cache read and write", 10, map[string]any{"cache_tokens": 40, "cache_creation_tokens": 50, "usage_semantic": "anthropic"}, 100, 40},
		{"no other info", 5, nil, 5, 0},
		{"decoded json numbers", 100, map[string]any{"cache_tokens": float64(30)}, 100, 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input, cache := channelStatInputTokens(tc.prompt, tc.other)
			assert.Equal(t, tc.wantInput, input)
			assert.Equal(t, tc.wantCache, cache)
		})
	}
}

func TestRecordConsumeLogCountsWhenLogDisabled(t *testing.T) {
	resetChannelStats(t)
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = true })

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	other := NewLogOther()
	other.SetPublic("cache_tokens", 20)
	RecordConsumeLog(c, 1, RecordConsumeLogParams{ChannelId: 11, PromptTokens: 80, Other: other})
	require.NoError(t, FlushChannelStats())

	got, err := GetChannelStatsSummaries([]int{11}, 0)
	require.NoError(t, err)
	require.Contains(t, got, 11)
	assert.Equal(t, ChannelStatsSummary{Success: 1, InputTokens: 80, CacheTokens: 20}, *got[11])
}
