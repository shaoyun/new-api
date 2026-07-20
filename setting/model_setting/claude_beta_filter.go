package model_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/config"
)

// Beta filter channel keys. These identify a group of upstream channel types
// that share the same anthropic-beta forwarding policy.
const (
	BetaFilterKeyAws       = "aws"
	BetaFilterKeyAnthropic = "anthropic"
	BetaFilterKeyVertex    = "vertex"
)

// BetaFilterRule describes how anthropic-beta flags are filtered for one
// channel key before they are forwarded upstream.
//
// When Enabled is false the raw flags are forwarded unchanged. When Enabled is
// true only flags present in the effective whitelist survive: ModelOverride[model]
// takes precedence when the requested model has an entry, otherwise Whitelist is used.
type BetaFilterRule struct {
	Enabled       bool                `json:"enabled"`
	Whitelist     []string            `json:"whitelist"`
	ModelOverride map[string][]string `json:"model_override"`
}

// ClaudeBetaFilterSettings is the centralized, per-channel-type configuration
// for anthropic-beta flag filtering. It is persisted via the shared option KV
// table (no dedicated schema) under the "claude_beta_filter" module.
type ClaudeBetaFilterSettings struct {
	Rules map[string]BetaFilterRule `json:"rules"`
}

// bedrockDefaultBetaWhitelist is the set of anthropic_beta values verified to be
// accepted by AWS Bedrock. Flags outside this list cause Bedrock to return 400,
// so both the native AWS channel and Anthropic-typed channels that ultimately
// relay to Bedrock share this whitelist.
var bedrockDefaultBetaWhitelist = []string{
	"claude-code-20250219",
	"interleaved-thinking-2025-05-14",
	"context-management-2025-06-27",
	"effort-2025-11-24",
	"computer-use-2025-11-24",
}

var defaultClaudeBetaFilterSettings = ClaudeBetaFilterSettings{
	Rules: map[string]BetaFilterRule{
		BetaFilterKeyAws: {
			Enabled:       true,
			Whitelist:     append([]string(nil), bedrockDefaultBetaWhitelist...),
			ModelOverride: map[string][]string{},
		},
		BetaFilterKeyAnthropic: {
			Enabled:       true,
			Whitelist:     append([]string(nil), bedrockDefaultBetaWhitelist...),
			ModelOverride: map[string][]string{},
		},
	},
}

var claudeBetaFilterSettings = cloneClaudeBetaFilterSettings(defaultClaudeBetaFilterSettings)

func init() {
	config.GlobalConfig.Register("claude_beta_filter", &claudeBetaFilterSettings)
}

// GetClaudeBetaFilterSettings returns the live beta filter configuration.
//
// This is a pure read (no lazy mutation) so it is safe to call concurrently
// from relay goroutines, matching the other model_setting getters. Reading a
// nil Rules map in FilterByChannelType is safe and yields passthrough.
func GetClaudeBetaFilterSettings() *ClaudeBetaFilterSettings {
	return &claudeBetaFilterSettings
}

// betaFilterKeyForChannelType maps a channel type to its beta filter key.
// An empty return value means the channel type has no filter policy and flags
// should be forwarded unchanged.
func betaFilterKeyForChannelType(channelType int) string {
	switch channelType {
	case constant.ChannelTypeAws:
		return BetaFilterKeyAws
	case constant.ChannelTypeAnthropic:
		return BetaFilterKeyAnthropic
	case constant.ChannelTypeVertexAi:
		return BetaFilterKeyVertex
	default:
		return ""
	}
}

// FilterByChannelType applies the beta filter policy for the given channel type
// and model to the raw anthropic-beta flags, returning the flags that may be
// forwarded upstream. Order and de-duplication of the input are preserved; an
// empty result means no flag should be sent.
func (c *ClaudeBetaFilterSettings) FilterByChannelType(channelType int, model string, rawFlags []string) []string {
	key := betaFilterKeyForChannelType(channelType)
	if key == "" {
		return dedupeNonEmpty(rawFlags)
	}
	rule, ok := c.Rules[key]
	if !ok || !rule.Enabled {
		return dedupeNonEmpty(rawFlags)
	}

	allowed := rule.Whitelist
	if override, exists := rule.ModelOverride[model]; exists {
		allowed = override
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, flag := range allowed {
		flag = strings.TrimSpace(flag)
		if flag != "" {
			allowedSet[flag] = struct{}{}
		}
	}

	filtered := make([]string, 0, len(rawFlags))
	seen := make(map[string]struct{}, len(rawFlags))
	for _, flag := range rawFlags {
		flag = strings.TrimSpace(flag)
		if flag == "" {
			continue
		}
		if _, dup := seen[flag]; dup {
			continue
		}
		if _, allow := allowedSet[flag]; !allow {
			continue
		}
		seen[flag] = struct{}{}
		filtered = append(filtered, flag)
	}
	return filtered
}

func dedupeNonEmpty(flags []string) []string {
	result := make([]string, 0, len(flags))
	seen := make(map[string]struct{}, len(flags))
	for _, flag := range flags {
		flag = strings.TrimSpace(flag)
		if flag == "" {
			continue
		}
		if _, dup := seen[flag]; dup {
			continue
		}
		seen[flag] = struct{}{}
		result = append(result, flag)
	}
	return result
}

func cloneClaudeBetaFilterSettings(src ClaudeBetaFilterSettings) ClaudeBetaFilterSettings {
	cloned := ClaudeBetaFilterSettings{Rules: make(map[string]BetaFilterRule, len(src.Rules))}
	for key, rule := range src.Rules {
		newRule := BetaFilterRule{
			Enabled:       rule.Enabled,
			Whitelist:     append([]string(nil), rule.Whitelist...),
			ModelOverride: make(map[string][]string, len(rule.ModelOverride)),
		}
		for model, flags := range rule.ModelOverride {
			newRule.ModelOverride[model] = append([]string(nil), flags...)
		}
		cloned.Rules[key] = newRule
	}
	return cloned
}
