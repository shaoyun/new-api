package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// DebugSetting controls the admin-only request debugging feature. When enabled,
// each relay request's headers (redacted) and body are captured into a
// short-lived cache keyed by request_id, so admins can inspect them from the
// usage log UI. Disabled by default because request bodies contain plaintext
// prompts and headers contain credentials.
type DebugSetting struct {
	RequestDebugEnabled bool `json:"request_debug_enabled"`
	// TTLMinutes is how long captured debug data is retained in cache.
	TTLMinutes int `json:"ttl_minutes"`
	// MaxBodyKB bounds the captured request body size; larger bodies are
	// truncated to avoid bloating the cache.
	MaxBodyKB int `json:"max_body_kb"`
}

var debugSetting = DebugSetting{
	RequestDebugEnabled: false,
	TTLMinutes:          15,
	MaxBodyKB:           256,
}

func init() {
	config.GlobalConfig.Register("debug_setting", &debugSetting)
}

func GetDebugSetting() *DebugSetting {
	return &debugSetting
}

// RequestDebugTTLMinutes returns the configured retention minutes, falling back
// to 15 when misconfigured to a non-positive value.
func (s *DebugSetting) RequestDebugTTLMinutes() int {
	if s.TTLMinutes <= 0 {
		return 15
	}
	return s.TTLMinutes
}

// RequestDebugMaxBodyBytes returns the body capture limit in bytes, falling back
// to 256 KB when misconfigured to a non-positive value.
func (s *DebugSetting) RequestDebugMaxBodyBytes() int {
	if s.MaxBodyKB <= 0 {
		return 256 * 1024
	}
	return s.MaxBodyKB * 1024
}
