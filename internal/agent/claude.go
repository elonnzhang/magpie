package agent

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yetone/dial/internal/catalog"
	"github.com/yetone/dial/internal/edit"
)

// Claude Code reads its endpoint from the `env` block of settings.json:
// ANTHROPIC_BASE_URL plus ANTHROPIC_AUTH_TOKEN point it at any vendor that
// speaks the Anthropic Messages API, and the ANTHROPIC_DEFAULT_*_MODEL
// variables tell it what "opus", "sonnet" and "haiku" mean over there.

var claudeAliases = []Option{
	{Value: "opus", Note: "alias · latest Opus"},
	{Value: "sonnet", Note: "alias · latest Sonnet"},
	{Value: "haiku", Note: "alias · latest Haiku"},
	{Value: "opusplan", Note: "alias · Opus for planning, Sonnet for work"},
	{Value: "sonnet[1m]", Note: "alias · Sonnet with 1M context"},
}

// env vars dial owns on a third-party provider; all go away on the way back.
var claudeEnv = []string{
	"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_SMALL_FAST_MODEL",
}

func claude(home string) *Agent {
	path := filepath.Join(home, ".claude", "settings.json")
	env := func(k string) string { v, _ := edit.GetJSON(path, "env."+k); return v }
	model := jsonGet(path, "model")

	// current provider: "anthropic" (no base URL), a preset id, or "custom".
	current := func() (string, *Provider) {
		base := env("ANTHROPIC_BASE_URL")
		if base == "" {
			return "anthropic", nil
		}
		if p := providerByURL(base, func(p Provider) string { return p.Anthropic }); p != nil {
			return p.ID, p
		}
		return "custom", nil
	}
	// write the model everywhere Claude Code looks for it on a third party.
	setModel := func(p *Provider, m string) error {
		kvs := []edit.KV{{Path: "model", Value: m}}
		if p != nil {
			small := p.Small
			if small == "" {
				small = m
			}
			kvs = append(kvs,
				edit.KV{Path: "env.ANTHROPIC_MODEL", Value: m},
				edit.KV{Path: "env.ANTHROPIC_DEFAULT_OPUS_MODEL", Value: m},
				edit.KV{Path: "env.ANTHROPIC_DEFAULT_SONNET_MODEL", Value: m},
				edit.KV{Path: "env.ANTHROPIC_DEFAULT_HAIKU_MODEL", Value: small},
				edit.KV{Path: "env.ANTHROPIC_SMALL_FAST_MODEL", Value: small},
			)
		}
		return edit.SetJSON(path, kvs...)
	}
	use := func(id string) error {
		cur, _ := current()
		switch id {
		case "custom":
			if cur != "custom" {
				return fmt.Errorf("custom means whatever ANTHROPIC_BASE_URL is already in %s; set it there", path)
			}
			return nil
		case "anthropic":
			keys := make([]string, len(claudeEnv))
			for i, k := range claudeEnv {
				keys[i] = "env." + k
			}
			if err := edit.DelJSON(path, keys...); err != nil {
				return err
			}
			// a vendor's model id means nothing to Anthropic: restore what was
			// set before leaving, else Claude Code's own default.
			if m := model(); m != "" && !strings.HasPrefix(m, "claude") && !isAlias(m) {
				if prev := unstash("claude.model"); prev != "" {
					return edit.SetJSON(path, edit.KV{Path: "model", Value: prev})
				}
				return edit.DelJSON(path, "model")
			}
			return nil
		}
		p := provider(id)
		if p == nil || p.Anthropic == "" {
			return fmt.Errorf("unknown provider %q", id)
		}
		key := Key(p.EnvKey)
		if key == "" {
			return needKey(*p)
		}
		if cur == "anthropic" {
			stash(map[string]string{"claude.model": model()})
		}
		p.refreshQuietly()
		if err := edit.SetJSON(path,
			edit.KV{Path: "env.ANTHROPIC_BASE_URL", Value: p.Anthropic},
			edit.KV{Path: "env.ANTHROPIC_AUTH_TOKEN", Value: key},
		); err != nil {
			return err
		}
		ms := p.Models()
		m := model()
		if !hasModel(ms, m) && len(ms) > 0 {
			m = ms[0].ID
		}
		return setModel(p, m)
	}

	return &Agent{
		ID: "claude", Name: "Claude Code", Icon: "claudecode-color", Aliases: []string{"cc", "claude-code"},
		Bin: "claude", Dir: filepath.Dir(path), Path: path,
		Fields: []Field{
			{
				Key: "provider", Label: "provider",
				Get: func() string { id, _ := current(); return id },
				Set: use,
				Options: func(map[string]string) []Option {
					out := []Option{{Value: "anthropic", Label: "Anthropic", Icon: "claude-color", Note: "Claude sign-in / $ANTHROPIC_API_KEY"}}
					var ps []Option
					for _, p := range Providers() {
						if p.Anthropic != "" {
							ps = append(ps, p.option(p.Anthropic))
						}
					}
					out = append(out, sortReady(ps)...)
					if id, _ := current(); id == "custom" {
						out = append(out, Option{Value: "custom", Note: hostOf(env("ANTHROPIC_BASE_URL")) + " (from settings.json)"})
					}
					return out
				},
			},
			{
				Key: "model", Label: "model",
				Get: model,
				Set: func(v string) error { _, p := current(); return setModel(p, v) },
				Options: func(map[string]string) []Option {
					if _, p := current(); p != nil {
						return options(p.Models(), "")
					}
					return append(append([]Option{}, claudeAliases...), options(catalog.Provider("anthropic"), "")...)
				},
			},
		},
	}
}

func isAlias(m string) bool {
	for _, a := range claudeAliases {
		if a.Value == m {
			return true
		}
	}
	return false
}

func hasModel(ms []catalog.Model, id string) bool {
	for _, m := range ms {
		if m.ID == id {
			return true
		}
	}
	return false
}
