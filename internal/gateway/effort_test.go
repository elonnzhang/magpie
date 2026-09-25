package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/provider"
)

func TestFitEffort(t *testing.T) {
	glm := []string{"low", "high", "max"}
	for _, c := range []struct {
		want   string
		levels []string
		got    string
	}{
		{"medium", glm, "high"}, // a tie goes up
		{"low", glm, "low"},
		{"xhigh", glm, "max"},
		{"max", []string{"low", "medium", "high"}, "high"},
		{"low", []string{"none", "minimal", "medium"}, "medium"},
		{"medium", nil, "medium"}, // levels not known: as asked
		{"medium", []string{"none"}, "medium"},
	} {
		if got := fitEffort(c.want, c.levels); got != c.got {
			t.Errorf("fitEffort(%q, %v) = %q, want %q", c.want, c.levels, got, c.got)
		}
	}
}

// Codex's effort reaches a custom OpenAI-compatible provider as
// reasoning_effort, at a level the model takes: glm-5.3-flash takes low,
// high and max, as models.dev has it from those serving it, so Codex's
// "medium" goes as "high"; a model no one gives levels for gets what was
// asked.
func TestResponsesEffortReachesChatVendor(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"id":"c1","model":"glm-5.3-flash","choices":[{"delta":{"role":"assistant","content":"hi"}}]}`,
		`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`)}
	up := setup(t, provider.Chat, f)
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{"zai":{"models":{"glm-5.3-flash":{"id":"glm-5.3-flash","reasoning_options":[{"type":"effort","values":["low","high","max"]}]}}}}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	if err := provider.Save(provider.Provider{ID: "volc", Name: "Volc", Key: "k", Chat: up.URL + "/v1", Models: []string{"glm-5.3-flash", "other"}}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ model, effort, sent string }{
		{"volc/glm-5.3-flash", "medium", "high"},
		{"volc/glm-5.3-flash", "max", "max"},
		{"volc/glm-5.3-flash", "low", "low"},
		{"volc/other", "medium", "medium"},
	} {
		code, body := post(t, "/v1/responses", `{"model":"`+c.model+`","stream":true,"input":"hi","reasoning":{"effort":"`+c.effort+`"}}`)
		if code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		var got map[string]any
		json.Unmarshal(f.got, &got)
		if got["reasoning_effort"] != c.sent || !strings.HasSuffix(f.path, "/chat/completions") {
			t.Errorf("%s at %s: sent %s", c.model, c.effort, f.got)
		}
	}
}
