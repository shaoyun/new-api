package aws

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDoAwsClientRequest_AppliesRuntimeHeaderOverrideToAnthropicBeta(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName:           "claude-3-5-sonnet-20240620",
		IsStream:                  false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"anthropic-beta": "computer-use-2025-01-24",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-3-5-sonnet-20240620",
		},
	}

	requestBody := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)

	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))

	anthropicBeta, exists := payload["anthropic_beta"]
	require.True(t, exists)

	values, ok := anthropicBeta.([]any)
	require.True(t, ok)
	require.Equal(t, []any{"computer-use-2025-01-24"}, values)
}

func TestStripUnsupportedOutputConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		in          string
		want        string // expected result; empty means nil/empty result
		wantRemoved bool   // whether the format field was reported as removed
		exactly     bool   // compare as raw bytes instead of JSON-equal
	}{
		{
			name:        "removes format only key drops object",
			in:          `{"format":{"type":"json_schema","schema":{"type":"object"}}}`,
			want:        "",
			wantRemoved: true,
		},
		{
			name:        "removes format preserves effort",
			in:          `{"effort":"high","format":{"type":"json_schema"}}`,
			want:        `{"effort":"high"}`,
			wantRemoved: true,
		},
		{
			name:        "no format is unchanged",
			in:          `{"effort":"high"}`,
			want:        `{"effort":"high"}`,
			wantRemoved: false,
		},
		{
			name:        "empty input unchanged",
			in:          ``,
			want:        "",
			wantRemoved: false,
		},
		{
			name:        "invalid json returned as-is",
			in:          `not-json`,
			want:        `not-json`,
			wantRemoved: false,
			exactly:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, removed := stripUnsupportedOutputConfig(json.RawMessage(tc.in))
			require.Equal(t, tc.wantRemoved, removed)
			if tc.want == "" {
				require.Empty(t, got)
				return
			}
			if tc.exactly {
				require.Equal(t, tc.want, string(got))
				return
			}
			require.JSONEq(t, tc.want, string(got))
		})
	}
}

func TestConvertClaudeRequestStripsOutputConfigFormat(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	adaptor := &Adaptor{}

	// Default: format stripped, object dropped when it becomes empty.
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	req := &dto.ClaudeRequest{
		Model:        "claude-sonnet-4-5",
		OutputConfig: json.RawMessage(`{"format":{"type":"json_schema","schema":{"type":"object"}}}`),
	}
	_, err := adaptor.ConvertClaudeRequest(ctx, info, req)
	require.NoError(t, err)
	require.Empty(t, req.OutputConfig)

	// effort is preserved after format removal.
	req.OutputConfig = json.RawMessage(`{"effort":"high","format":{"type":"json_schema"}}`)
	_, err = adaptor.ConvertClaudeRequest(ctx, info, req)
	require.NoError(t, err)
	require.JSONEq(t, `{"effort":"high"}`, string(req.OutputConfig))

	// Legacy top-level output_format is also stripped by default.
	legacyReq := &dto.ClaudeRequest{
		Model:        "claude-sonnet-4-5",
		OutputFormat: json.RawMessage(`{"type":"json_schema","schema":{"type":"object"}}`),
	}
	_, err = adaptor.ConvertClaudeRequest(ctx, info, legacyReq)
	require.NoError(t, err)
	require.Empty(t, legacyReq.OutputFormat)

	// Opt-in: passthrough leaves both output_config and output_format untouched.
	allowInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	allowInfo.ChannelOtherSettings.AllowOutputConfigFormat = true
	allowReq := &dto.ClaudeRequest{
		Model:        "claude-sonnet-4-5",
		OutputConfig: json.RawMessage(`{"format":{"type":"json_schema"}}`),
		OutputFormat: json.RawMessage(`{"type":"json_schema"}`),
	}
	_, err = adaptor.ConvertClaudeRequest(ctx, allowInfo, allowReq)
	require.NoError(t, err)
	require.JSONEq(t, `{"format":{"type":"json_schema"}}`, string(allowReq.OutputConfig))
	require.JSONEq(t, `{"type":"json_schema"}`, string(allowReq.OutputFormat))
}
