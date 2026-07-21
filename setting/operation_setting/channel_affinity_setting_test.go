package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClaudeCliPassThroughHeadersExcludesAnthropicBeta locks in a billing/relay
// safety invariant: the "claude cli trace" channel-affinity rule must NOT pass
// through the raw client "Anthropic-Beta" header. That pass_headers operation
// runs AFTER SetupRequestHeader (where the claude_beta_filter whitelist strips
// unsupported flags), so restoring the original header would re-inject
// unsupported beta flags and cause Bedrock-backed upstreams to return
// 400 "invalid beta flag". Beta flags must be governed solely by the whitelist.
func TestClaudeCliPassThroughHeadersExcludesAnthropicBeta(t *testing.T) {
	for _, h := range claudeCliPassThroughHeaders {
		assert.NotEqualf(t, "anthropic-beta", normalizeHeaderName(h),
			"claudeCliPassThroughHeaders must not pass through %q; it bypasses the claude_beta_filter whitelist", h)
	}
}

// TestClaudeCliTraceRuleUsesPassThroughTemplate guards that the "claude cli
// trace" rule still builds its param override from claudeCliPassThroughHeaders,
// so the exclusion above actually governs what the live rule passes through.
func TestClaudeCliTraceRuleUsesPassThroughTemplate(t *testing.T) {
	var found bool
	for _, rule := range channelAffinitySetting.Rules {
		if rule.Name == "claude cli trace" {
			found = true
			require.NotNil(t, rule.ParamOverrideTemplate, "claude cli trace rule must have a param override template")
			ops, ok := rule.ParamOverrideTemplate["operations"].([]map[string]interface{})
			require.True(t, ok, "param override template must contain operations")
			require.NotEmpty(t, ops)
			passed, ok := ops[0]["value"].([]string)
			require.True(t, ok, "pass_headers operation value must be a []string")
			for _, h := range passed {
				assert.NotEqual(t, "anthropic-beta", normalizeHeaderName(h),
					"claude cli trace rule must not pass through anthropic-beta")
			}
		}
	}
	require.True(t, found, "claude cli trace rule must exist")
}

func normalizeHeaderName(h string) string {
	b := []byte(h)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
