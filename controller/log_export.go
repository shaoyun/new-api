package controller

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// exportBatchSize is the keyset page size used when streaming the billing
// export. Rows are fetched, serialized, and flushed one batch at a time so
// memory stays flat regardless of total export size.
const exportBatchSize = 1000

// billingExportHeader is the CSV column order for the admin billing export.
// Kept in sync with the frontend export page description.
var billingExportHeader = []string{
	"时间",         // created_at, formatted in Asia/Shanghai
	"用户",         // username
	"令牌",         // token_name
	"模型",         // model_name
	"输入token",    // prompt_tokens
	"输出token",    // completion_tokens
	"缓存读token",  // other.cache_tokens
	"缓存写token",  // other.cache_creation_tokens
	"总费用USD",    // quota / QuotaPerUnit
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
func formatQuotaUSD(quota int) string {
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

// ExportLogs streams an admin billing export as a CSV attachment. It requires a
// time window and a non-empty comma-separated username list, and includes both
// successful (type 2) and error (type 5) rows. Data is fetched via keyset
// pagination and flushed batch by batch so memory stays flat for large exports.
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
	writer.Flush()

	var lastCreatedAt int64
	var lastId int
	for {
		logs, err := model.GetLogsForExport(startTimestamp, endTimestamp, usernames, lastCreatedAt, lastId, exportBatchSize)
		if err != nil {
			// Headers are already sent; log server-side and stop the stream.
			common.SysError("billing export query failed: " + err.Error())
			return
		}
		if len(logs) == 0 {
			break
		}
		for _, log := range logs {
			other, _ := common.StrToMap(log.Other)
			record := []string{
				time.Unix(log.CreatedAt, 0).In(loc).Format("2006/1/2 15:04:05"),
				log.Username,
				log.TokenName,
				log.ModelName,
				strconv.Itoa(log.PromptTokens),
				strconv.Itoa(log.CompletionTokens),
				strconv.Itoa(otherInt(other, "cache_tokens")),
				strconv.Itoa(otherInt(other, "cache_creation_tokens")),
				formatQuotaUSD(log.Quota),
			}
			if err := writer.Write(record); err != nil {
				return
			}
			lastCreatedAt = log.CreatedAt
			lastId = log.Id
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			common.SysError("billing export write failed: " + err.Error())
			return
		}
		if len(logs) < exportBatchSize {
			break
		}
	}
}
