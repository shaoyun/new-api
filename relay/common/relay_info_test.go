package common

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestRemoveDisabledFieldsContextManagement(t *testing.T) {
	const body = `{"model":"claude-opus-4-8","context_management":{"edits":[{"type":"clear_thinking_20251015","keep":"all"}]},"max_tokens":1024}`

	t.Run("stripped by default", func(t *testing.T) {
		out, err := RemoveDisabledFields([]byte(body), dto.ChannelOtherSettings{}, false)
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(out, "context_management").Exists())
		require.Equal(t, "claude-opus-4-8", gjson.GetBytes(out, "model").String())
		require.Equal(t, int64(1024), gjson.GetBytes(out, "max_tokens").Int())
	})

	t.Run("preserved when explicitly allowed", func(t *testing.T) {
		out, err := RemoveDisabledFields([]byte(body), dto.ChannelOtherSettings{AllowContextManagement: true}, false)
		require.NoError(t, err)
		require.True(t, gjson.GetBytes(out, "context_management.edits.0.type").Exists())
		require.Equal(t, "clear_thinking_20251015", gjson.GetBytes(out, "context_management.edits.0.type").String())
	})

	t.Run("preserved when channel passthrough enabled", func(t *testing.T) {
		out, err := RemoveDisabledFields([]byte(body), dto.ChannelOtherSettings{}, true)
		require.NoError(t, err)
		require.True(t, gjson.GetBytes(out, "context_management").Exists())
	})
}

func TestRemoveDisabledFieldsOutputConfigFormat(t *testing.T) {
	t.Run("format stripped by default, sibling keys preserved", func(t *testing.T) {
		const body = `{"model":"claude-opus-4-8","output_config":{"format":{"type":"json_schema"},"effort":"high"},"max_tokens":1024}`
		out, err := RemoveDisabledFields([]byte(body), dto.ChannelOtherSettings{}, false)
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(out, "output_config.format").Exists())
		require.Equal(t, "high", gjson.GetBytes(out, "output_config.effort").String())
		require.Equal(t, "claude-opus-4-8", gjson.GetBytes(out, "model").String())
	})

	t.Run("output_config dropped when format was its only key", func(t *testing.T) {
		const body = `{"output_config":{"format":{"type":"json_schema"}},"max_tokens":1024}`
		out, err := RemoveDisabledFields([]byte(body), dto.ChannelOtherSettings{}, false)
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(out, "output_config").Exists())
	})

	t.Run("preserved when explicitly allowed", func(t *testing.T) {
		const body = `{"output_config":{"format":{"type":"json_schema"}},"max_tokens":1024}`
		out, err := RemoveDisabledFields([]byte(body), dto.ChannelOtherSettings{AllowOutputConfigFormat: true}, false)
		require.NoError(t, err)
		require.Equal(t, "json_schema", gjson.GetBytes(out, "output_config.format.type").String())
	})
}
