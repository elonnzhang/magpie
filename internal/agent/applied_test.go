package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/usage"
)

// Something else rewrote Codex's config: the base URL gone while the model
// is still magpie's is unwired, and setting it again wires it back.
func TestCodexDriftUnwired(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "")
	cx := codex(home)
	if err := cx.Apply("model", "fake/m1"); err != nil {
		t.Fatal(err)
	}
	if d := cx.Drift(); d != nil {
		t.Fatalf("drift right after a set: %+v", d)
	}
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"fake/m1\"\n"), 0o644)
	d := cx.Drift()
	if d == nil || d.Kind != "unwired" || d.Want != "fake/m1" {
		t.Fatalf("unwired: %+v", d)
	}
	if err := cx.Reapply(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, "openai_base_url") || cx.Drift() != nil {
		t.Fatalf("reapplied:\n%s", cfg)
	}
}

// A magpie model replaced by one of the agent's own is drift; one magpie
// model for another (the agent's own picker) isn't. Keep forgets it.
func TestDriftReplaced(t *testing.T) {
	home, read := codexHome(t, "", "")
	cx := codex(home)
	if err := cx.Apply("model", "fake/m1"); err != nil {
		t.Fatal(err)
	}
	// a switcher put Codex back on OpenAI, wholesale
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"gpt-5.5\"\nmodel_provider = \"openai\"\n"), 0o644)
	d := cx.Drift()
	if d == nil || d.Kind != "replaced" || d.Want != "fake/m1" || d.Now != "gpt-5.5" {
		t.Fatalf("replaced: %+v", d)
	}
	if err := cx.Reapply(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, `model = "fake/m1"`) || !strings.Contains(cfg, `model_provider = "magpie"`) || cx.Drift() != nil {
		t.Fatalf("reapplied:\n%s", cfg)
	}
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"gpt-5.5\"\n"), 0o644)
	cx.Keep()
	if d := cx.Drift(); d != nil {
		t.Fatalf("kept: %+v", d)
	}
}

// A Codex used since magpie set it, none of whose requests reached the
// gateway, runs round magpie: bypassed, until a request arrives or it is
// set again. A use while magpie wasn't up says nothing.
func TestDriftBypassed(t *testing.T) {
	home, _ := codexHome(t, "", "")
	cx := codex(home)
	defer func(s time.Time) { started = s }(started)
	started = time.Now().Add(-time.Hour)
	if err := cx.Apply("model", "fake/m1"); err != nil {
		t.Fatal(err)
	}
	// Apply stamped now; put it back to before the prompt
	m := appliedLoad()
	a := m["codex"]
	a.At = time.Now().Add(-10 * time.Minute)
	m["codex"] = a
	appliedSave(m)
	hist := filepath.Join(home, ".codex", "history.jsonl")
	prompt := func(at time.Time) {
		os.WriteFile(hist, []byte(fmt.Sprintf(`{"session_id":"s","ts":%d,"text":"hi"}`+"\n", at.Unix())), 0o600)
	}
	prompt(time.Now().Add(-5 * time.Minute))
	if d := cx.Drift(); d == nil || d.Kind != "bypassed" || d.Want != "fake/m1" {
		t.Fatalf("bypassed: %+v", d)
	}
	usage.Saw("codex")
	if d := cx.Drift(); d != nil {
		t.Fatalf("a request arrived: %+v", d)
	}
	prompt(time.Now().Add(-time.Minute))
	if d := cx.Drift(); d != nil {
		t.Fatalf("used before the last request: %+v", d)
	}
	started = time.Now()
	prompt(time.Now().Add(-5 * time.Minute))
	if d := cx.Drift(); d != nil {
		t.Fatalf("used while magpie was down: %+v", d)
	}
}

// A profile that sets its own provider wins over magpie's top level.
func TestCodexDriftProfile(t *testing.T) {
	home, read := codexHome(t, "", "")
	cx := codex(home)
	if err := cx.Apply("model", "fake/m1"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("profile = \"work\"\n"+read()+"\n[profiles.work]\nmodel_provider = \"openai\"\n"), 0o644)
	if d := cx.Drift(); d == nil || d.Kind != "unwired" || !strings.Contains(d.Detail, "profile work") {
		t.Fatalf("profile: %+v\n%s", d, read())
	}
}

// Every agent magpie wires through the gateway says when its wiring was
// taken out behind magpie's back — the gateway's URL swapped for another —
// and setting it again puts it back.
func TestDriftUnwiredEveryAgent(t *testing.T) {
	home, _ := codexHome(t, "", "")
	managed := claudeManaged
	claudeManaged = func() string { return filepath.Join(home, "managed-settings.json") }
	t.Cleanup(func() { claudeManaged = managed })
	for _, a := range All() {
		if a.Check == nil {
			continue
		}
		t.Run(a.ID, func(t *testing.T) {
			// the field that takes one of magpie's models
			var f Field
			want := ""
			for _, g := range a.Fields {
				for _, o := range g.Options(a.Values()) {
					if o.Ref == "fake/m1" && want == "" {
						f, want = g, o.Value
					}
				}
			}
			if want == "" {
				t.Fatal("no field takes magpie's models")
			}
			if err := a.Apply(f.Key, want); err != nil {
				t.Fatal(err)
			}
			if d := a.Drift(); d != nil {
				t.Fatalf("drift right after a set: %+v", d)
			}
			// something else points the agent at another server
			n := 0
			filepath.WalkDir(home, func(p string, e os.DirEntry, err error) error {
				if err != nil || e.IsDir() {
					return nil
				}
				b, _ := os.ReadFile(p)
				if s := strings.ReplaceAll(string(b), gateway.URL(), "http://127.0.0.1:9"); s != string(b) {
					os.WriteFile(p, []byte(s), 0o644)
					n++
				}
				return nil
			})
			if n == 0 {
				t.Fatal("no file holds the gateway's URL")
			}
			d := a.Drift()
			if d == nil || d.Kind != "unwired" {
				t.Fatalf("unwired: %+v", d)
			}
			if err := a.Reapply(); err != nil {
				t.Fatal(err)
			}
			if d := a.Drift(); d != nil {
				t.Fatalf("still drifted after Apply again: %+v", d)
			}
		})
	}
}

// Claude Code's managed settings win over the user's: a base URL there is
// drift magpie can only point out.
func TestClaudeDriftManaged(t *testing.T) {
	home, _ := codexHome(t, "", "")
	managed := claudeManaged
	claudeManaged = func() string { return filepath.Join(home, "managed-settings.json") }
	t.Cleanup(func() { claudeManaged = managed })
	cl := claude(home)
	if err := cl.Apply("model", "fake/m1"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(claudeManaged(), []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://corp.example"}}`), 0o644)
	if d := cl.Drift(); d == nil || d.Kind != "unwired" || !strings.Contains(d.Detail, "managed") {
		t.Fatalf("managed: %+v", d)
	}
}
