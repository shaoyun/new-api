package controller

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// exportBatchSize is the keyset page size used when reading logs for the
// billing export. Rows are fetched one batch at a time and folded straight into
// the daily buckets, so raw detail rows never accumulate in memory.
const exportBatchSize = 1000

// billingExportHeader is the CSV column order for the admin billing export.
// Kept in sync with the frontend export page description.
var billingExportHeader = []string{
	"日期",       // day bucket in Asia/Shanghai
	"用户",       // username
	"令牌",       // token_name
	"模型",       // model_name
	"输入token",  // sum(prompt_tokens)
	"输出token",  // sum(completion_tokens)
	"缓存读token", // sum(other.cache_tokens)
	"缓存写token", // sum(other.cache_creation_tokens)
	"总费用USD",   // sum(quota) / QuotaPerUnit
}

// billingExportKey identifies one aggregated export line: a single calendar day
// of one user's token calling one model.
type billingExportKey struct {
	DayStart  int64
	Username  string
	TokenName string
	ModelName string
}

// billingExportRow is the summed usage behind a billingExportKey. Totals are
// int64 because a day of requests sums well past the int32 range of the
// per-request columns they come from.
type billingExportRow struct {
	billingExportKey
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	Quota            int64
}

// parseUsernames splits a comma-separated username list, trimming blanks and
// de-duplicating while preserving order.
func parseUsernames(raw string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// otherInt reads an integer field from a decoded `other` JSON map, tolerating
// the float64 that JSON numbers decode into. Returns 0 when absent.
func otherInt(other map[string]interface{}, key string) int {
	if other == nil {
		return 0
	}
	switch v := other[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

// formatQuotaUSD renders a quota integer as its USD string, trimming trailing
// zeros to match the export-user-calls reference format.
func formatQuotaUSD(quota int64) string {
	usd := float64(quota) / common.QuotaPerUnit
	s := strconv.FormatFloat(usd, 'f', 6, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-0" {
		s = "0"
	}
	return s
}

// foldBillingExportBatch accumulates one keyset page of logs into the daily
// export buckets, keyed by (day in loc, username, token, model).
//
// Totals widen to int64 on the way in: the per-request columns are int32-bound
// by the billing invariants, but a day of requests sums past that range.
func foldBillingExportBatch(buckets map[billingExportKey]*billingExportRow, logs []*model.Log, loc *time.Location) {
	for _, log := range logs {
		ts := time.Unix(log.CreatedAt, 0).In(loc)
		key := billingExportKey{
			DayStart:  time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, loc).Unix(),
			Username:  log.Username,
			TokenName: log.TokenName,
			ModelName: log.ModelName,
		}
		row := buckets[key]
		if row == nil {
			row = &billingExportRow{billingExportKey: key}
			buckets[key] = row
		}
		other, _ := common.StrToMap(log.Other)
		row.PromptTokens += int64(log.PromptTokens)
		row.CompletionTokens += int64(log.CompletionTokens)
		row.CacheReadTokens += int64(otherInt(other, "cache_tokens"))
		row.CacheWriteTokens += int64(otherInt(other, "cache_creation_tokens"))
		row.Quota += int64(log.Quota)
	}
}

// sortedBillingExportRows flattens the buckets into CSV order: chronological,
// then a stable grouping within each day so repeated exports of the same window
// produce byte-identical files.
func sortedBillingExportRows(buckets map[billingExportKey]*billingExportRow) []*billingExportRow {
	rows := make([]*billingExportRow, 0, len(buckets))
	for _, row := range buckets {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.DayStart != b.DayStart {
			return a.DayStart < b.DayStart
		}
		if a.Username != b.Username {
			return a.Username < b.Username
		}
		if a.TokenName != b.TokenName {
			return a.TokenName < b.TokenName
		}
		return a.ModelName < b.ModelName
	})
	return rows
}

// ExportLogs serves an admin billing export as a CSV attachment. It requires a
// time window and a non-empty comma-separated username list, and includes both
// successful (type 2) and error (type 5) rows.
//
// Rows are aggregated to one line per (day, user, token, model). The daily
// bucket and the cache-token columns both rule out doing this in SQL: the day
// boundary must land in Asia/Shanghai (no portable date-truncation across
// SQLite/MySQL/PostgreSQL/ClickHouse) and the cache counters live inside the
// `other` JSON text column. So logs are still scanned with keyset pagination,
// but each batch is folded into the bucket map instead of being written out —
// only the aggregate is held in memory, and the CSV is written once at the end.
func ExportLogs(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	usernames := parseUsernames(c.Query("usernames"))

	if startTimestamp == 0 || endTimestamp == 0 {
		common.ApiErrorMsg(c, "start_timestamp and end_timestamp are required")
		return
	}
	if endTimestamp < startTimestamp {
		common.ApiErrorMsg(c, "end_timestamp must be greater than or equal to start_timestamp")
		return
	}
	if len(usernames) == 0 {
		common.ApiErrorMsg(c, "usernames is required")
		return
	}

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}

	buckets := make(map[billingExportKey]*billingExportRow)
	var lastCreatedAt int64
	var lastId int
	for {
		logs, err := model.GetLogsForExport(startTimestamp, endTimestamp, usernames, lastCreatedAt, lastId, exportBatchSize)
		if err != nil {
			// Nothing has been written yet, so this can still fail cleanly.
			common.SysError("billing export query failed: " + err.Error())
			common.ApiErrorMsg(c, "导出失败，请稍后重试")
			return
		}
		if len(logs) == 0 {
			break
		}
		foldBillingExportBatch(buckets, logs, loc)
		last := logs[len(logs)-1]
		lastCreatedAt, lastId = last.CreatedAt, last.Id
		if len(logs) < exportBatchSize {
			break
		}
	}

	rows := sortedBillingExportRows(buckets)

	filename := fmt.Sprintf("billing_export_%d_%d.csv", startTimestamp, endTimestamp)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Status(http.StatusOK)

	// UTF-8 BOM so Excel opens Chinese headers/values without mojibake.
	if _, err := c.Writer.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return
	}

	writer := csv.NewWriter(c.Writer)
	if err := writer.Write(billingExportHeader); err != nil {
		return
	}
	for _, row := range rows {
		record := []string{
			time.Unix(row.DayStart, 0).In(loc).Format("2006/1/2"),
			row.Username,
			row.TokenName,
			row.ModelName,
			strconv.FormatInt(row.PromptTokens, 10),
			strconv.FormatInt(row.CompletionTokens, 10),
			strconv.FormatInt(row.CacheReadTokens, 10),
			strconv.FormatInt(row.CacheWriteTokens, 10),
			formatQuotaUSD(row.Quota),
		}
		if err := writer.Write(record); err != nil {
			return
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		common.SysError("billing export write failed: " + err.Error())
	}
}
