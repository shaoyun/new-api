package model_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBetaFilter() *ClaudeBetaFilterSettings {
	return &ClaudeBetaFilterSettings{
		Rules: map[string]BetaFilterRule{
			BetaFilterKeyAws: {
				Enabled:   true,
				Whitelist: []string{"claude-code-20250219", "effort-2025-11-24"},
				ModelOverride: map[string][]string{
					"claude-opus-4-6": {"context-management-2025-06-27"},
				},
			},
			BetaFilterKeyAnthropic: {
				Enabled:   true,
				Whitelist: []string{"claude-code-20250219"},
			},
			BetaFilterKeyVertex: {
				Enabled:   false,
				Whitelist: []string{"claude-code-20250219"},
			},
		},
	}
}

func TestFilterByChannelType(t *testing.T) {
	f := newTestBetaFilter()
	cases := []struct {
		name        string
		channelType int
		model       string
		raw         []string
		want        []string
	}{
		{
			name:        "aws drops unsupported flag keeps whitelisted",
			channelType: constant.ChannelTypeAws,
			model:       "claude-sonnet-4-6",
			raw:         []string{"claude-code-20250219", "prompt-caching-scope-2026-01-05", "effort-2025-11-24"},
			want:        []string{"claude-code-20250219", "effort-2025-11-24"},
		},
		{
			name:        "aws model override replaces base whitelist",
			channelType: constant.ChannelTypeAws,
			model:       "claude-opus-4-6",
			raw:         []string{"context-management-2025-06-27", "claude-code-20250219"},
			want:        []string{"context-management-2025-06-27"},
		},
		{
			name:        "anthropic channel shares bedrock-style whitelist",
			channelType: constant.ChannelTypeAnthropic,
			model:       "claude-sonnet-4-6",
			raw:         []string{"claude-code-20250219", "redact-thinking-2026-02-12"},
			want:        []string{"claude-code-20250219"},
		},
		{
			name:        "disabled rule forwards unchanged (deduped)",
			channelType: constant.ChannelTypeVertexAi,
			model:       "claude-sonnet-4-6",
			raw:         []string{"anything-goes", "anything-goes", "another"},
			want:        []string{"anything-goes", "another"},
		},
		{
			name:        "unknown channel type forwards unchanged",
			channelType: 999,
			model:       "claude-sonnet-4-6",
			raw:         []string{"whatever-2026-01-01"},
			want:        []string{"whatever-2026-01-01"},
		},
		{
			name:        "all flags filtered out yields empty",
			channelType: constant.ChannelTypeAws,
			model:       "claude-sonnet-4-6",
			raw:         []string{"prompt-caching-scope-2026-01-05"},
			want:        []string{},
		},
		{
			name:        "whitespace trimmed and duplicates removed",
			channelType: constant.ChannelTypeAws,
			model:       "claude-sonnet-4-6",
			raw:         []string{" claude-code-20250219 ", "claude-code-20250219", ""},
			want:        []string{"claude-code-20250219"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := f.FilterByChannelType(tc.channelType, tc.model, tc.raw)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestDefaultSettingsShareBedrockWhitelist guards the invariant that the native
// AWS channel and Anthropic-typed channels (which ultimately relay to Bedrock)
// default to the same verified whitelist and are both enabled.
func TestDefaultSettingsShareBedrockWhitelist(t *testing.T) {
	def := cloneClaudeBetaFilterSettings(defaultClaudeBetaFilterSettings)
	awsRule := def.Rules[BetaFilterKeyAws]
	antRule := def.Rules[BetaFilterKeyAnthropic]

	assert.True(t, awsRule.Enabled)
	assert.True(t, antRule.Enabled)
	assert.Equal(t, awsRule.Whitelist, antRule.Whitelist)
	assert.Equal(t, bedrockDefaultBetaWhitelist, awsRule.Whitelist)
}

// TestPersistenceRoundTrip guards the exact contract that the settings UI and
// the option KV store must agree on: the value persisted under
// "claude_beta_filter.rules" is the bare rules map, and feeding it back through
// UpdateConfigFromMap must reconstruct the same per-channel rules. A regression
// here (e.g. an extra {"rules":...} wrapper) silently drops all rules and lets
// unsupported beta flags reach Bedrock.
func TestPersistenceRoundTrip(t *testing.T) {
	// This is what the frontend serializeRules() writes: the bare map, unwrapped.
	persisted := `{"aws":{"enabled":true,"whitelist":["claude-code-20250219"],"model_override":{}},` +
		`"anthropic":{"enabled":true,"whitelist":["effort-2025-11-24"],"model_override":{"claude-opus-4-6":["claude-code-20250219"]}}}`

	s := &ClaudeBetaFilterSettings{Rules: map[string]BetaFilterRule{}}
	require.NoError(t, config.UpdateConfigFromMap(s, map[string]string{"rules": persisted}))

	awsRule, ok := s.Rules[BetaFilterKeyAws]
	require.True(t, ok, "aws rule must survive the round-trip")
	assert.True(t, awsRule.Enabled)
	assert.Equal(t, []string{"claude-code-20250219"}, awsRule.Whitelist)

	antRule, ok := s.Rules[BetaFilterKeyAnthropic]
	require.True(t, ok, "anthropic rule must survive the round-trip")
	assert.Equal(t, []string{"claude-code-20250219"}, antRule.ModelOverride["claude-opus-4-6"])

	// An unsupported flag must still be dropped for AWS after reload.
	got := s.FilterByChannelType(constant.ChannelTypeAws, "claude-sonnet-4-6",
		[]string{"claude-code-20250219", "prompt-caching-scope-2026-01-05"})
	assert.Equal(t, []string{"claude-code-20250219"}, got)
}
