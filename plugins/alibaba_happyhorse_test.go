package plugins_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// HappyHorse rides the Wan async video API on Alibaba (type 17) channels. It
// is billed per second by output resolution, so the plugin must report
// seconds/resolution both up front (reservation) and from usage on completion.
func TestAlibabaHappyHorse(t *testing.T) {
	source, err := builtinplugins.Source("alibaba")
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "alibaba"})
	require.NoError(t, err)

	roundTrip := func(t *testing.T, value any) map[string]any {
		encoded, marshalErr := common.Marshal(value)
		require.NoError(t, marshalErr)
		var decoded map[string]any
		require.NoError(t, common.Unmarshal(encoded, &decoded))
		return decoded
	}
	submit := func(model string, body map[string]any) (map[string]any, error) {
		body["model"] = model
		value, callErr := plugin.Engine.Call(t.Context(), "buildSubmitRequest", map[string]any{
			"model": model, "upstreamModel": model, "baseUrl": "https://dashscope.aliyuncs.com", "apiKey": "k", "requestBody": body,
		})
		if callErr != nil {
			return nil, callErr
		}
		return roundTrip(t, value), nil
	}
	facts := func(model string, body map[string]any) map[string]any {
		body["model"] = model
		value, callErr := plugin.Engine.Call(t.Context(), "extractUsage", map[string]any{
			"model": model, "upstreamModel": model, "usagePurpose": "facts", "requestBody": body,
		})
		require.NoError(t, callErr)
		return roundTrip(t, value)
	}

	t.Run("all six models are declared", func(t *testing.T) {
		for _, model := range []string{"happyhorse-1.1-t2v", "happyhorse-1.1-i2v", "happyhorse-1.1-r2v", "happyhorse-1.0-t2v", "happyhorse-1.0-i2v", "happyhorse-1.0-r2v"} {
			assert.Contains(t, plugin.Meta.Models, model)
		}
	})

	t.Run("i2v sends one first_frame on the async video endpoint without Wan-only params", func(t *testing.T) {
		req, err := submit("happyhorse-1.1-i2v", map[string]any{"prompt": "a cat runs", "image": "https://cdn.example/first.png", "resolution": "720P", "duration": 5})
		require.NoError(t, err)
		assert.Equal(t, "https://dashscope.aliyuncs.com/api/v1/services/aigc/video-generation/video-synthesis", req["url"])
		assert.Equal(t, "enable", req["headers"].(map[string]any)["X-DashScope-Async"])
		body := req["body"].(map[string]any)
		assert.Equal(t, "happyhorse-1.1-i2v", body["model"])
		input := body["input"].(map[string]any)
		assert.Equal(t, []any{map[string]any{"type": "first_frame", "url": "https://cdn.example/first.png"}}, input["media"])
		assert.NotContains(t, input, "img_url")
		assert.Equal(t, map[string]any{"resolution": "720P", "duration": float64(5)}, body["parameters"],
			"no prompt_extend/size, and no ratio for i2v (it follows the first frame)")
	})

	t.Run("t2v keeps ratio and rejects media; r2v maps images to reference_image", func(t *testing.T) {
		req, err := submit("happyhorse-1.1-t2v", map[string]any{"prompt": "a cat", "ratio": "9:16", "resolution": "480P", "duration": 8})
		require.NoError(t, err)
		params := req["body"].(map[string]any)["parameters"].(map[string]any)
		assert.Equal(t, map[string]any{"resolution": "480P", "ratio": "9:16", "duration": float64(8)}, params)

		_, err = submit("happyhorse-1.1-t2v", map[string]any{"prompt": "a cat", "metadata": map[string]any{"input": map[string]any{"media": []any{map[string]any{"type": "first_frame", "url": "u"}}}}})
		require.ErrorContains(t, err, "does not accept input.media")

		req, err = submit("happyhorse-1.1-r2v", map[string]any{"prompt": "[Image 1] waves", "images": []any{"https://cdn.example/a.png", "https://cdn.example/b.png"}})
		require.NoError(t, err)
		input := req["body"].(map[string]any)["input"].(map[string]any)
		assert.Equal(t, []any{
			map[string]any{"type": "reference_image", "url": "https://cdn.example/a.png"},
			map[string]any{"type": "reference_image", "url": "https://cdn.example/b.png"},
		}, input["media"])
	})

	t.Run("bounds: duration 3-15, model resolutions, media counts, prompt required", func(t *testing.T) {
		img := "https://cdn.example/first.png"
		for _, d := range []any{2, 16, 4.5} {
			_, err := submit("happyhorse-1.1-i2v", map[string]any{"prompt": "p", "image": img, "duration": d})
			require.ErrorContains(t, err, "between 3 and 15", "duration %v", d)
		}
		_, err := submit("happyhorse-1.0-i2v", map[string]any{"prompt": "p", "image": img, "resolution": "480P"})
		require.ErrorContains(t, err, "resolution must be one of 720P, 1080P", "1.0 has no 480P tier")
		_, err = submit("happyhorse-1.1-i2v", map[string]any{"prompt": "p"})
		require.ErrorContains(t, err, "exactly one first_frame")
		ten := make([]any, 10)
		for i := range ten {
			ten[i] = img
		}
		_, err = submit("happyhorse-1.1-r2v", map[string]any{"prompt": "p", "images": ten})
		require.ErrorContains(t, err, "1 to 9 reference_image")
		_, err = submit("happyhorse-1.1-i2v", map[string]any{"image": img})
		require.ErrorContains(t, err, "requires prompt")
	})

	t.Run("reservation facts come from the request, defaulting to 5s at 1080P", func(t *testing.T) {
		assert.Equal(t, map[string]any{"seconds": float64(5), "resolution": "720P"},
			facts("happyhorse-1.1-i2v", map[string]any{"prompt": "p", "image": "https://cdn.example/a.png", "resolution": "720p", "duration": 5}))
		assert.Equal(t, map[string]any{"seconds": float64(5), "resolution": "1080P"},
			facts("happyhorse-1.1-t2v", map[string]any{"prompt": "p"}))
	})

	t.Run("settlement facts come from usage.duration and usage.SR", func(t *testing.T) {
		value, err := plugin.Engine.Call(t.Context(), "extractUsageOnComplete",
			map[string]any{"model": "happyhorse-1.1-i2v", "upstreamModel": "happyhorse-1.1-i2v"}, map[string]any{},
			map[string]any{
				"output": map[string]any{"task_status": "SUCCEEDED", "video_url": "https://upstream.example/v.mp4"},
				"usage":  map[string]any{"duration": 5, "input_video_duration": 0, "output_video_duration": 5, "video_count": 1, "SR": 720},
			})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"seconds": float64(5), "resolution": "720P"}, roundTrip(t, value))
	})
}
