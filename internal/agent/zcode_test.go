package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

func TestZCode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".zcode", "v2", "config.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"provider":{"builtin:bigmodel":{"name":"Bigmodel","kind":"anthropic","enabled":true}}}`), 0o644)
	read := func() map[string]any {
		var c struct {
			Provider map[string]map[string]any `json:"provider"`
		}
		b, _ := os.ReadFile(path)
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		if c.Provider["builtin:bigmodel"] == nil {
			t.Fatalf("ZCode's own provider went: %s", b)
		}
		return c.Provider["magpie"]
	}

	a := zcode(home)
	if !a.Detected() {
		t.Fatal("not detected")
	}
	f := a.Field("provider")
	if f.Get() != "" {
		t.Fatalf("get: %q", f.Get())
	}
	if err := f.Set("magpie"); err != nil {
		t.Fatal(err)
	}
	m := read()
	opts, _ := m["options"].(map[string]any)
	models, _ := m["models"].(map[string]any)
	pro, _ := models["deepseek/pro"].(map[string]any)
	if m["kind"] != "anthropic" || m["enabled"] != true || m["source"] != "custom" || opts["apiKey"] != "magpie" ||
		opts["baseURL"] == "" || pro == nil || pro["limit"] == nil || pro["modalities"] == nil {
		t.Fatalf("magpie provider: %v", m)
	}
	if f.Get() != "magpie" {
		t.Fatalf("get: %q", f.Get())
	}

	// a provider added later reaches ZCode's picker
	if err := provider.Save(provider.Provider{ID: "kimi", Name: "Kimi", Chat: "https://api.moonshot.cn/v1", Key: "k", Models: []string{"k2"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if models, _ := read()["models"].(map[string]any); models["kimi/k2"] == nil {
		t.Fatalf("not synced: %v", models)
	}

	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if read() != nil || f.Get() != "" {
		t.Fatal("magpie provider left behind")
	}
	// nothing to sync into a config that has no magpie provider
	if err := a.Sync(); err != nil || read() != nil {
		t.Fatal("sync added the provider")
	}
}
