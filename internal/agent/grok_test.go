package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/provider"
)

func TestGrok(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("GROK_HOME", filepath.Join(home, "grok"))
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "grok", "config.toml")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("# mine\n[ui]\ntheme = \"dark\"\n\n[model.my-own]\nmodel = \"x\"\nbase_url = \"https://x/v1\"\n\n[models]\ndefault = \"grok-4.6\"\n"), 0o644)
	read := func() string { b, _ := os.ReadFile(path); return string(b) }
	a := grok(home)
	if a.Path != path {
		t.Fatalf("path: %s", a.Path)
	}
	f := a.Field("model")
	if f.Get() != "grok-4.6" {
		t.Fatalf("get: %q", f.Get())
	}

	if err := f.Set("magpie/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	raw := read()
	m := edit.GetTOMLTable(path, `model."magpie/deepseek/pro"`)
	if m["model"] != "deepseek/pro" || m["api_key"] != "magpie" || !strings.HasSuffix(m["base_url"], "/v1") ||
		m["api_backend"] != "chat_completions" || edit.GetTOMLTable(path, `model."magpie/deepseek/flash"`) == nil {
		t.Fatalf("tables:\n%s", raw)
	}
	// a campaign of xAI's would set the default over it
	if edit.GetTOMLTable(path, "features")["campaigns"] != "false" {
		t.Fatalf("campaigns:\n%s", raw)
	}
	if f.Get() != "magpie/deepseek/pro" || !strings.Contains(raw, "# mine") || !strings.Contains(raw, "[model.my-own]") ||
		edit.GetTOMLTable(path, "ui")["theme"] != "dark" {
		t.Fatalf("config:\n%s", raw)
	}

	// picked again, and synced: the tables are replaced, never repeated
	if err := f.Set("magpie/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	raw = read()
	if n := strings.Count(raw, `[model."magpie/deepseek/pro"]`); n != 1 {
		t.Fatalf("%d tables:\n%s", n, raw)
	}

	// a provider removed leaves Grok's list on the next sync
	provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"flash"}})
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(), `"magpie/deepseek/pro"]`) {
		t.Fatalf("stale:\n%s", read())
	}

	// its own model: magpie steps out, the user's own model stays
	if err := f.Set("grok-4.6"); err != nil {
		t.Fatal(err)
	}
	raw = read()
	if f.Get() != "grok-4.6" || strings.Contains(raw, "magpie") || !strings.Contains(raw, "[model.my-own]") {
		t.Fatalf("own:\n%s", raw)
	}
	// nothing through magpie: sync leaves the file alone
	if err := a.Sync(); err != nil || read() != raw {
		t.Fatalf("sync touched it:\n%s", read())
	}

	e := a.Field("effort")
	f.Set("magpie/deepseek/flash")
	if err := e.Set("high"); err != nil || e.Get() != "high" {
		t.Fatalf("effort: %v %q", err, e.Get())
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	raw = read()
	if f.Get() != "" || strings.Contains(raw, "magpie") || strings.Contains(raw, "campaigns") || e.Get() != "high" {
		t.Fatalf("reset:\n%s", raw)
	}
}
