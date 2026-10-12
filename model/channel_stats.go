package model

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelStat 按 (渠道, 整点小时) 累计的请求结果与缓存用量，用于渠道列表展示近 24 小时的
// 成功率与缓存命中率。计数在内存中累加，定时以累加语义 upsert，多实例并发写入也正确。
type ChannelStat struct {
	Id           int   `json:"id"`
	ChannelId    int   `json:"channel_id" gorm:"uniqueIndex:idx_channel_stat_bucket"`
	BucketTs     int64 `json:"bucket_ts" gorm:"uniqueIndex:idx_channel_stat_bucket;index"`
	SuccessCount int64 `json:"success_count"`
	FailCount    int64 `json:"fail_count"`
	InputTokens  int64 `json:"input_tokens"`
	CacheTokens  int64 `json:"cache_tokens"`
}

// ChannelStatsSummary 是渠道在统计窗口内的原始计数，比率由前端计算。
type ChannelStatsSummary struct {
	Success     int64 `json:"success"`
	Fail        int64 `json:"fail"`
	InputTokens int64 `json:"input_tokens"`
	CacheTokens int64 `json:"cache_tokens"`
}

const (
	channelStatsBucketSeconds = 3600
	channelStatsWindowBuckets = 24
	channelStatsRetention     = 7 * 24 * time.Hour
	channelStatsFlushInterval = time.Minute
)

type channelStatKey struct {
	channelId int
	bucketTs  int64
}

var (
	channelStatsMu      sync.Mutex
	channelStatsPending = map[channelStatKey]*ChannelStatsSummary{}
	channelStatsOnce    sync.Once
)

// AddChannelStat 在内存中累加一次统计，不访问数据库。
func AddChannelStat(channelId int, success, fail, inputTokens, cacheTokens int64) {
	if channelId <= 0 {
		return
	}
	key := channelStatKey{channelId: channelId, bucketTs: common.GetTimestamp() / channelStatsBucketSeconds * channelStatsBucketSeconds}
	channelStatsMu.Lock()
	defer channelStatsMu.Unlock()
	s := channelStatsPending[key]
	if s == nil {
		s = &ChannelStatsSummary{}
		channelStatsPending[key] = s
	}
	s.Success += success
	s.Fail += fail
	s.InputTokens += inputTokens
	s.CacheTokens += cacheTokens
}

// FlushChannelStats 把内存中的计数累加写入 channel_stats；写入失败的部分放回内存，下次重试。
func FlushChannelStats() error {
	channelStatsMu.Lock()
	pending := channelStatsPending
	channelStatsPending = map[channelStatKey]*ChannelStatsSummary{}
	channelStatsMu.Unlock()

	var firstErr error
	failed := map[channelStatKey]*ChannelStatsSummary{}
	for key, s := range pending {
		row := ChannelStat{
			ChannelId:    key.channelId,
			BucketTs:     key.bucketTs,
			SuccessCount: s.Success,
			FailCount:    s.Fail,
			InputTokens:  s.InputTokens,
			CacheTokens:  s.CacheTokens,
		}
		err := DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "channel_id"}, {Name: "bucket_ts"}},
			DoUpdates: clause.Assignments(map[string]any{
				"success_count": gorm.Expr("channel_stats.success_count + ?", s.Success),
				"fail_count":    gorm.Expr("channel_stats.fail_count + ?", s.Fail),
				"input_tokens":  gorm.Expr("channel_stats.input_tokens + ?", s.InputTokens),
				"cache_tokens":  gorm.Expr("channel_stats.cache_tokens + ?", s.CacheTokens),
			}),
		}).Create(&row).Error
		if err == nil {
			continue
		}
		if firstErr == nil {
			firstErr = err
		}
		failed[key] = s
	}
	if len(failed) == 0 {
		return nil
	}
	channelStatsMu.Lock()
	defer channelStatsMu.Unlock()
	for key, s := range failed {
		cur := channelStatsPending[key]
		if cur == nil {
			channelStatsPending[key] = s
			continue
		}
		cur.Success += s.Success
		cur.Fail += s.Fail
		cur.InputTokens += s.InputTokens
		cur.CacheTokens += s.CacheTokens
	}
	return firstErr
}

// GetChannelStatsSummaries 汇总指定渠道 bucket_ts >= since 的计数；无数据的渠道不出现在结果中。
func GetChannelStatsSummaries(channelIds []int, since int64) (map[int]*ChannelStatsSummary, error) {
	result := make(map[int]*ChannelStatsSummary, len(channelIds))
	if len(channelIds) == 0 {
		return result, nil
	}
	var rows []struct {
		ChannelId   int
		Success     int64
		Fail        int64
		InputTokens int64
		CacheTokens int64
	}
	err := DB.Model(&ChannelStat{}).
		Select("channel_id, SUM(success_count) AS success, SUM(fail_count) AS fail, SUM(input_tokens) AS input_tokens, SUM(cache_tokens) AS cache_tokens").
		Where("channel_id IN ? AND bucket_ts >= ?", channelIds, since).
		Group("channel_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.ChannelId] = &ChannelStatsSummary{Success: r.Success, Fail: r.Fail, InputTokens: r.InputTokens, CacheTokens: r.CacheTokens}
	}
	return result, nil
}

// AttachChannelStats 为渠道列表附上近 24 小时（当前小时 + 前 23 个整点桶）的统计。
// 查询失败只记录日志，不影响列表返回。
func AttachChannelStats(channels []*Channel) {
	if len(channels) == 0 {
		return
	}
	ids := make([]int, 0, len(channels))
	for _, ch := range channels {
		ids = append(ids, ch.Id)
	}
	since := common.GetTimestamp()/channelStatsBucketSeconds*channelStatsBucketSeconds - (channelStatsWindowBuckets-1)*channelStatsBucketSeconds
	summaries, err := GetChannelStatsSummaries(ids, since)
	if err != nil {
		common.SysError("failed to load channel stats: " + err.Error())
		return
	}
	for _, ch := range channels {
		ch.Stats = summaries[ch.Id]
	}
}

// StartChannelStatsFlusher 启动后台定时 flush，并按小时清理超过保留期的桶。只会启动一次。
func StartChannelStatsFlusher() {
	channelStatsOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(channelStatsFlushInterval)
			defer ticker.Stop()
			lastCleanup := time.Time{}
			for range ticker.C {
				if err := FlushChannelStats(); err != nil {
					common.SysError("failed to flush channel stats: " + err.Error())
				}
				if time.Since(lastCleanup) < time.Hour {
					continue
				}
				lastCleanup = time.Now()
				before := time.Now().Add(-channelStatsRetention).Unix()
				if err := DB.Where("bucket_ts < ?", before).Delete(&ChannelStat{}).Error; err != nil {
					common.SysError("failed to cleanup channel stats: " + err.Error())
				}
			}
		}()
	})
}

// channelStatInputTokens 按厂商语义归一输入 token（缓存命中率分母），口径与日志统计的
// logStatTotalInputTokensExpr 一致：Anthropic 语义 prompt 不含缓存，需加上缓存读写；
// 其余厂商 prompt 已包含缓存。
func channelStatInputTokens(promptTokens int, other map[string]any) (input, cache int64) {
	toInt64 := func(v any) int64 {
		switch n := v.(type) {
		case int:
			return int64(n)
		case int64:
			return n
		case float64:
			return int64(n)
		}
		return 0
	}
	input = int64(promptTokens)
	cache = toInt64(other["cache_tokens"])
	if other["usage_semantic"] == "anthropic" {
		input += cache + toInt64(other["cache_creation_tokens"])
	}
	return input, cache
}
