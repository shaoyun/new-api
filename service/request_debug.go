package service

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
)

const requestDebugCacheNamespace = "new-api:request_debug:v1"

// requestDebugMemoryCapacity bounds the in-memory fallback (no Redis) so large
// bodies cannot grow the cache without limit.
const requestDebugMemoryCapacity = 512

// RequestDebugPayload is the admin-only snapshot of a single relay request.
type RequestDebugPayload struct {
	RequestID     string            `json:"request_id"`
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	Model         string            `json:"model"`
	Headers       map[string]string `json:"headers"`
	Body          string            `json:"body"`
	BodyTruncated bool              `json:"body_truncated"`
	CapturedAt    int64             `json:"captured_at"`
}

var (
	requestDebugCacheOnce sync.Once
	requestDebugCache     *cachex.HybridCache[string]
)

func getRequestDebugCache() *cachex.HybridCache[string] {
	requestDebugCacheOnce.Do(func() {
		ttl := time.Duration(operation_setting.GetDebugSetting().RequestDebugTTLMinutes()) * time.Minute
		requestDebugCache = cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
			Namespace: cachex.Namespace(requestDebugCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.StringCodec{},
			Memory: func() *hot.HotCache[string, string] {
				return hot.NewHotCache[string, string](hot.LRU, requestDebugMemoryCapacity).
					WithTTL(ttl).
					WithJanitor().
					Build()
			},
		})
	})
	return requestDebugCache
}

// sensitiveHeaderKeys are request headers whose values must never be exposed
// verbatim to admins (they carry credentials).
var sensitiveHeaderKeys = map[string]bool{
	"authorization":          true,
	"x-api-key":              true,
	"x-goog-api-key":         true,
	"mj-api-secret":          true,
	"sec-websocket-protocol": true,
	"cookie":                 true,
	"set-cookie":             true,
	"proxy-authorization":    true,
}

// redactHeaders masks credential-bearing headers, keeping a short suffix hint so
// admins can still correlate which key was used without leaking it.
func redactHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		if sensitiveHeaderKeys[strings.ToLower(strings.TrimSpace(k))] {
			out[k] = maskSecretValue(v)
			continue
		}
		out[k] = v
	}
	return out
}

func maskSecretValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if len(v) <= 8 {
		return "[REDACTED]"
	}
	return "****" + v[len(v)-4:]
}

// CaptureRequestDebug snapshots the current request into the debug cache when
// the admin toggle is on. It never blocks or fails the main request: work runs
// in a background goroutine with a recover guard, and any error is swallowed.
func CaptureRequestDebug(c *gin.Context, relayInfo *relaycommon.RelayInfo) {
	if c == nil || relayInfo == nil {
		return
	}
	setting := operation_setting.GetDebugSetting()
	if !setting.RequestDebugEnabled {
		return
	}
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		return
	}

	// Read the body synchronously while it is still available (BodyStorage is
	// cleaned up after the handler returns); marshal/store off the hot path.
	maxBytes := setting.RequestDebugMaxBodyBytes()
	body, truncated := readRequestBodyForDebug(c, maxBytes)
	headers := redactHeaders(relayInfo.RequestHeaders)

	method := ""
	path := ""
	if c.Request != nil {
		method = c.Request.Method
		if c.Request.URL != nil {
			path = c.Request.URL.Path
		}
	}

	payload := RequestDebugPayload{
		RequestID:     requestID,
		Method:        method,
		Path:          path,
		Model:         relayInfo.OriginModelName,
		Headers:       headers,
		Body:          body,
		BodyTruncated: truncated,
		CapturedAt:    time.Now().Unix(),
	}

	gopool.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				common.SysError(fmt.Sprintf("request debug capture panic: %v", r))
			}
		}()
		raw, err := common.Marshal(payload)
		if err != nil {
			return
		}
		ttl := time.Duration(setting.RequestDebugTTLMinutes()) * time.Minute
		_ = getRequestDebugCache().SetWithTTL(requestID, string(raw), ttl)
	})
}

func readRequestBodyForDebug(c *gin.Context, maxBytes int) (string, bool) {
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil {
		return "", false
	}
	data, err := storage.Bytes()
	if err != nil || len(data) == 0 {
		return "", false
	}
	if maxBytes > 0 && len(data) > maxBytes {
		return string(data[:maxBytes]), true
	}
	return string(data), false
}

// GetRequestDebug returns the cached debug payload for a request id.
func GetRequestDebug(requestID string) (*RequestDebugPayload, bool) {
	if requestID == "" {
		return nil, false
	}
	raw, found, err := getRequestDebugCache().Get(requestID)
	if err != nil || !found || raw == "" {
		return nil, false
	}
	var payload RequestDebugPayload
	if err := common.UnmarshalJsonStr(raw, &payload); err != nil {
		return nil, false
	}
	return &payload, true
}
