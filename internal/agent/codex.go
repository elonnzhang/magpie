package agent

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yetone/dial/internal/catalog"
	"github.com/yetone/dial/internal/edit"
)

// Codex talks the OpenAI Responses API to whichever provider config.toml
// names. Switching provider means three things: a [model_providers.<id>]
// table, `model_provider = "<id>"`, and a model catalog file so the models
// show up inside Codex's own /model picker.

// codexPrompt is Codex's generic system prompt (Apache-2.0, openai/codex,
// core/gpt-5.2-codex_prompt.md). Third-party models need one because the
// bundled catalog only carries prompts for OpenAI models.
//
//go:embed codex_prompt.md
var codexPrompt string

// CodexProvider is an endpoint Codex can be pointed at.
type CodexProvider struct {
	ID      string
	Name    string
	BaseURL string // must speak the Responses API at <BaseURL>/responses
	EnvKey  string // environment variable holding the API key
	Catalog string // models.dev provider id for the model list
	Icon    string
	dial    *Provider // the dial provider behind a preset
	custom  bool      // found in config.toml, not a dial preset
}

// codexPresets are "openai" (Codex's own default, expressed by the absence
// of model_provider) plus every provider with a Responses endpoint.
func codexPresets() []CodexProvider {
	out := []CodexProvider{{ID: "openai", Name: "OpenAI", Icon: "openai"}}
	for _, p := range Providers() {
		if p.Responses != "" {
			out = append(out, CodexProvider{ID: p.ID, Name: p.Name, BaseURL: p.Responses, EnvKey: p.EnvKey, Catalog: p.Catalog, Icon: p.Icon, dial: &p})
		}
	}
	return out
}

func codex(home string) *Agent {
	dir := filepath.Join(home, ".codex")
	path := filepath.Join(dir, "config.toml")
	catalogPath := filepath.Join(dir, "dial-models.json")
	get := func(k string) string { v, _ := edit.GetTOMLTop(path, k); return v }

	providers := func() []CodexProvider {
		out := codexPresets()
		for _, t := range edit.TOMLTables(path) {
			id, ok := strings.CutPrefix(t, "model_providers.")
			if !ok || strings.Contains(id, ".") {
				continue
			}
			if p := findProvider(out, id); p != nil {
				continue
			}
			kv := edit.GetTOMLTable(path, t)
			out = append(out, CodexProvider{ID: id, Name: kv["name"], BaseURL: kv["base_url"], EnvKey: kv["env_key"], custom: true})
		}
		return out
	}
	current := func() CodexProvider {
		id := get("model_provider")
		if id == "" {
			id = "openai"
		}
		if p := findProvider(providers(), id); p != nil {
			return *p
		}
		return CodexProvider{ID: id, Name: id, custom: true}
	}
	models := func(p CodexProvider) []catalog.Model {
		switch {
		case p.ID == "openai":
			return catalog.Codex()
		case p.dial != nil:
			return p.dial.Models()
		}
		return nil
	}
	// keep model and effort valid for the provider; a fresh provider gets
	// its first model and that model's default effort.
	settle := func(p CodexProvider) error {
		ms := models(p)
		if len(ms) == 0 {
			return nil
		}
		model, effort := get("model"), get("model_reasoning_effort")
		found := false
		for _, m := range ms {
			if m.ID == model {
				found = true
			}
		}
		if !found {
			model = ms[0].ID
		}
		var kvs []edit.KV
		if model != get("model") {
			kvs = append(kvs, edit.KV{Path: "model", Value: model})
		}
		if e := catalog.Efforts(ms, model); len(e) > 0 && !contains(e, effort) {
			kvs = append(kvs, edit.KV{Path: "model_reasoning_effort", Value: defaultEffort(e)})
		}
		if len(kvs) == 0 {
			return nil
		}
		return edit.SetTOMLTop(path, kvs...)
	}
	use := func(id string) error {
		p := findProvider(providers(), id)
		if p == nil {
			return fmt.Errorf("unknown provider %q", id)
		}
		if p.ID == "openai" {
			if err := edit.DelTOMLTop(path, "model_provider", "model_catalog_json"); err != nil {
				return err
			}
			os.Remove(catalogPath)
			if m := unstash("codex.model"); m != "" && !hasModel(models(*p), get("model")) {
				var kvs []edit.KV
				kvs = append(kvs, edit.KV{Path: "model", Value: m})
				if e := unstash("codex.effort"); e != "" {
					kvs = append(kvs, edit.KV{Path: "model_reasoning_effort", Value: e})
				}
				if err := edit.SetTOMLTop(path, kvs...); err != nil {
					return err
				}
			}
			return settle(*p)
		}
		if current().ID == "openai" {
			stash(map[string]string{"codex.model": get("model"), "codex.effort": get("model_reasoning_effort")})
		}
		if !p.custom {
			key := Key(p.EnvKey)
			if key == "" {
				return needKey(Provider{Name: p.Name, EnvKey: p.EnvKey})
			}
			p.dial.refreshQuietly()
			kvs := []edit.KV{
				{Path: "name", Value: p.Name},
				{Path: "base_url", Value: p.BaseURL},
				{Path: "env_key", Value: p.EnvKey},
				{Path: "wire_api", Value: "responses"},
			}
			// Codex reads env_key from its own environment; a key that only
			// dial holds (desktop app, no shell) rides along as a bearer token.
			if os.Getenv(p.EnvKey) == "" {
				kvs = append(kvs, edit.KV{Path: "experimental_bearer_token", Value: key})
			}
			if err := edit.SetTOMLTable(path, "model_providers."+p.ID, kvs...); err != nil {
				return err
			}
		}
		kvs := []edit.KV{{Path: "model_provider", Value: p.ID}}
		if ms := models(*p); len(ms) > 0 {
			if err := edit.WriteAtomic(catalogPath, codexCatalog(ms, *p)); err != nil {
				return err
			}
			kvs = append(kvs, edit.KV{Path: "model_catalog_json", Value: catalogPath})
		} else if err := edit.DelTOMLTop(path, "model_catalog_json"); err != nil {
			return err
		}
		if err := edit.SetTOMLTop(path, kvs...); err != nil {
			return err
		}
		return settle(*p)
	}

	return &Agent{
		ID: "codex", Name: "Codex", Icon: "codex-color", Bin: "codex", Dir: dir, Path: path,
		// the app-server behind the Codex app (and every codex TUI) builds
		// its model list once, at start-up.
		Notice: func() string {
			if Running(`(^|/)codex( |$)`) {
				return "Codex builds its model list at start-up — restart the Codex app (and open codex sessions) to see this."
			}
			return ""
		},
		Fields: []Field{
			{
				Key: "provider", Label: "provider",
				Get: func() string { return current().ID },
				Set: use,
				Options: func(map[string]string) []Option {
					var out []Option
					for _, p := range providers() {
						o := Option{Value: p.ID, Label: p.Name, Note: p.note(), Key: p.EnvKey, Icon: p.Icon}
						o.NeedKey = p.EnvKey != "" && Key(p.EnvKey) == ""
						out = append(out, o)
					}
					return append(out[:1], sortReady(out[1:])...)
				},
			},
			{
				Key: "model", Label: "model",
				Get: func() string { return get("model") },
				Set: func(v string) error {
					if err := edit.SetTOMLTop(path, edit.KV{Path: "model", Value: v}); err != nil {
						return err
					}
					return settle(current())
				},
				Options: func(map[string]string) []Option { return options(models(current()), "") },
			},
			{
				Key: "effort", Label: "effort",
				Get: func() string { return get("model_reasoning_effort") },
				Set: func(v string) error {
					return edit.SetTOMLTop(path, edit.KV{Path: "model_reasoning_effort", Value: v})
				},
				Options: func(cur map[string]string) []Option {
					if e := catalog.Efforts(models(current()), cur["model"]); len(e) > 0 {
						return static(e...)
					}
					return static("low", "medium", "high", "xhigh")
				},
			},
		},
	}
}

func (p CodexProvider) note() string {
	switch {
	case p.ID == "openai":
		return "ChatGPT sign-in or OPENAI_API_KEY"
	case p.EnvKey != "" && Key(p.EnvKey) == "":
		return p.host() + " · $" + p.EnvKey + " not set"
	case p.EnvKey != "":
		return p.host() + " · $" + p.EnvKey
	}
	return p.host()
}

func (p CodexProvider) host() string { return hostOf(p.BaseURL) }

func findProvider(ps []CodexProvider, id string) *CodexProvider {
	for i := range ps {
		if ps[i].ID == id {
			return &ps[i]
		}
	}
	return nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// defaultEffort picks the middle of the road: "medium" or "high" when
// offered, else whatever the list starts with.
func defaultEffort(e []string) string {
	for _, want := range []string{"medium", "high"} {
		if contains(e, want) {
			return want
		}
	}
	return e[0]
}

// codexCatalog renders models in the shape of Codex's models.json so
// `model_catalog_json` can point at it. Only fields Codex requires or that
// change behaviour are set; the rest take Codex's defaults.
func codexCatalog(ms []catalog.Model, p CodexProvider) []byte {
	type level struct {
		Effort      string `json:"effort"`
		Description string `json:"description"`
	}
	type model struct {
		Slug          string  `json:"slug"`
		DisplayName   string  `json:"display_name"`
		Description   string  `json:"description"`
		Instructions  string  `json:"base_instructions"`
		DefaultEffort *string `json:"default_reasoning_level"`
		Efforts       []level `json:"supported_reasoning_levels"`
		Shell         string  `json:"shell_type"`
		Visibility    string  `json:"visibility"`
		InAPI         bool    `json:"supported_in_api"`
		Priority      int     `json:"priority"`
		Verbosity     bool    `json:"support_verbosity"`
		DefVerbosity  *string `json:"default_verbosity"`
		ApplyPatch    string  `json:"apply_patch_tool_type"`
		Truncation    struct {
			Mode  string `json:"mode"`
			Limit int    `json:"limit"`
		} `json:"truncation_policy"`
		Tools      []string `json:"experimental_supported_tools"`
		Modalities []string `json:"input_modalities"`
	}
	var out struct {
		Models []model `json:"models"`
	}
	for i, m := range ms {
		e := model{
			Slug: m.ID, DisplayName: m.Name, Description: m.Name + " via " + p.host(),
			Instructions: codexPrompt, Efforts: []level{},
			Shell: "unified_exec", Visibility: "list", InAPI: true, Priority: i + 1,
			ApplyPatch: "freeform", Tools: []string{}, Modalities: []string{"text"},
		}
		e.Truncation.Mode, e.Truncation.Limit = "tokens", 10000
		for _, ef := range m.Efforts {
			e.Efforts = append(e.Efforts, level{Effort: ef})
		}
		if len(m.Efforts) > 0 {
			d := defaultEffort(m.Efforts)
			e.DefaultEffort = &d
		}
		out.Models = append(out.Models, e)
	}
	b, _ := json.MarshalIndent(out, "", " ")
	return b
}
