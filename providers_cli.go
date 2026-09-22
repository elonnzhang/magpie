package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/yetone/dial/internal/agent"
)

var amber = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#F2B544"})

const providerUsage = `usage:
  dial providers                          list providers, keys and who uses them
  dial provider <id>                      show one provider
  dial provider add <name> k=v…           k: anthropic, responses, env, catalog, models, website, keys
  dial provider edit <id> k=v…            change fields of a provider (built-ins get an edited copy)
  dial provider rm <id>                   delete a custom provider, or hide a built-in
  dial provider reset <id>                built-in back to dial's defaults (also un-hides it)
  dial provider test <id>                 send a tiny request through each endpoint
  dial provider models <id>               fetch the vendor's model list and print it

  e.g. dial provider add "My Gateway" anthropic=https://gw.example.com/anthropic env=GW_API_KEY catalog=anthropic`

// providers: `dial providers`
func providers() error {
	agents := agent.Detected()
	type row struct{ name, id, host, key, models, uses string }
	var rows []row
	w := [5]int{}
	for _, p := range agent.Providers() {
		r := row{name: bold.Render(p.Name), id: muted.Render(p.ID), host: p.Host()}
		if !p.Builtin {
			r.name += " " + faint.Render("custom")
		}
		switch state, _ := agent.KeyState(p.EnvKey); state {
		case "env":
			r.key = green.Render("●") + " $" + p.EnvKey
		case "stored":
			r.key = green.Render("●") + " " + p.EnvKey + muted.Render(" (stored)")
		default:
			r.key = amber.Render("○ no key")
		}
		n := len(p.Models())
		if live, t, ok := p.LiveModels(); ok {
			r.models = fmt.Sprintf("%d models", live) + muted.Render(" · fetched "+ago(t))
		} else {
			r.models = fmt.Sprintf("%d models", n)
		}
		var uses []string
		for _, a := range agents {
			if f := a.Field("provider"); f != nil && f.Get() == p.ID {
				uses = append(uses, a.Name)
			}
		}
		if len(uses) > 0 {
			r.uses = green.Render("← " + strings.Join(uses, ", "))
		}
		for i, s := range []string{r.name, r.id, r.host, r.key, r.models} {
			w[i] = max(w[i], lipgloss.Width(s))
		}
		rows = append(rows, r)
	}
	for _, r := range rows {
		fmt.Printf("  %s  %s  %s  %s  %s  %s\n", pad(r.name, w[0]), pad(r.id, w[1]), pad(r.host, w[2]), pad(r.key, w[3]), pad(r.models, w[4]), r.uses)
	}
	if h := agent.HiddenProviders(); len(h) > 0 {
		names := make([]string, 0, len(h))
		for _, p := range h {
			names = append(names, p.ID)
		}
		fmt.Println(faint.Render("  hidden: " + strings.Join(names, ", ") + "  (dial provider reset <id>)"))
	}
	return nil
}

// provider: `dial provider <verb> …`
func provider(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("%s", providerUsage)
	}
	verb, rest := args[1], args[2:]
	switch verb {
	case "add":
		if len(rest) == 0 {
			return fmt.Errorf("dial provider add <name> k=v…\n\n%s", providerUsage)
		}
		p := agent.Provider{Name: rest[0]}
		if err := applyPairs(&p, rest[1:]); err != nil {
			return err
		}
		if err := agent.SaveProvider(p); err != nil {
			return err
		}
		p.ID = agent.ProviderID(first(p.ID, p.Name))
		fmt.Println(green.Render("✓"), "added", p.Name, muted.Render("("+p.ID+")"))
		if agent.Key(p.EnvKey) == "" {
			fmt.Println(muted.Render("  set a key:"), "dial key", p.EnvKey, "…")
		}
		return nil
	case "edit":
		if len(rest) < 2 {
			return fmt.Errorf("dial provider edit <id> k=v…\n\n%s", providerUsage)
		}
		p, err := agent.FindProvider(rest[0])
		if err != nil {
			return err
		}
		if err := applyPairs(p, rest[1:]); err != nil {
			return err
		}
		if err := agent.SaveProvider(*p); err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "saved", p.Name)
		return nil
	case "rm", "remove", "delete", "hide":
		if len(rest) != 1 {
			return fmt.Errorf("dial provider rm <id>")
		}
		p, err := agent.FindProvider(rest[0])
		if err != nil {
			return err
		}
		if err := agent.DeleteProvider(p.ID); err != nil {
			return err
		}
		if p.Builtin {
			fmt.Println(green.Render("✓"), "hid", p.Name, muted.Render("— dial provider reset "+p.ID+" brings it back"))
		} else {
			fmt.Println(green.Render("✓"), "deleted", p.Name)
		}
		return nil
	case "reset":
		if len(rest) != 1 {
			return fmt.Errorf("dial provider reset <id>")
		}
		if err := agent.ResetProvider(rest[0]); err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), rest[0], "is back to dial's defaults")
		return nil
	case "test":
		if len(rest) != 1 {
			return fmt.Errorf("dial provider test <id>")
		}
		p, err := agent.FindProvider(rest[0])
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ok := true
		for _, r := range p.Test(ctx) {
			if r.OK {
				fmt.Printf("  %s %-10s %s\n", green.Render("✓"), r.Protocol, muted.Render(fmt.Sprintf("%d ms · %s", r.Millis, r.Model)))
			} else {
				ok = false
				msg := r.Error
				if r.Status != 0 {
					msg = fmt.Sprintf("%d · %s", r.Status, r.Error)
				}
				fmt.Printf("  %s %-10s %s\n", amber.Render("✗"), r.Protocol, msg)
			}
		}
		if !ok {
			os.Exit(1)
		}
		return nil
	case "models":
		if len(rest) != 1 {
			return fmt.Errorf("dial provider models <id>")
		}
		p, err := agent.FindProvider(rest[0])
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ms, err := p.RefreshModels(ctx)
		if err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), len(ms), "models from", p.Host())
		return showProvider(*p)
	}
	// `dial provider <id>`
	p, err := agent.FindProvider(verb)
	if err != nil {
		return err
	}
	return showProvider(*p)
}

func showProvider(p agent.Provider) error {
	kv := func(k, v string) {
		if v != "" {
			fmt.Printf("  %s %s\n", muted.Render(pad(k, 10)), v)
		}
	}
	name := bold.Render(p.Name) + muted.Render("  "+p.ID)
	if !p.Builtin {
		name += faint.Render("  custom")
	}
	fmt.Println(" ", name)
	kv("anthropic", p.Anthropic)
	kv("responses", p.Responses)
	state, masked := agent.KeyState(p.EnvKey)
	switch state {
	case "env":
		kv("key", "$"+p.EnvKey+" "+muted.Render(masked))
	case "stored":
		kv("key", p.EnvKey+" "+muted.Render(masked+" · stored"))
	default:
		kv("key", amber.Render("$"+p.EnvKey+" is not set")+muted.Render("  dial key "+p.EnvKey+" …"))
	}
	kv("catalog", p.Catalog)
	kv("website", p.Website)
	kv("keys", p.KeysURL)
	ms := p.Models()
	src := "catalog"
	if _, t, ok := p.LiveModels(); ok {
		src = p.Host() + " · fetched " + ago(t)
	}
	kv("models", fmt.Sprintf("%d %s", len(ms), muted.Render("from "+src)))
	for i, m := range ms {
		if i == 12 {
			fmt.Println(faint.Render(fmt.Sprintf("             … %d more", len(ms)-12)))
			break
		}
		line := "             " + m.ID
		if m.Name != "" && m.Name != m.ID {
			line += muted.Render("  " + m.Name)
		}
		fmt.Println(line)
	}
	return nil
}

func applyPairs(p *agent.Provider, pairs []string) error {
	for _, kv := range pairs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("expected key=value, got %q\n\n%s", kv, providerUsage)
		}
		switch strings.ToLower(k) {
		case "id":
			p.ID = v
		case "name":
			p.Name = v
		case "anthropic":
			p.Anthropic = v
		case "responses", "openai":
			p.Responses = v
		case "env", "key":
			p.EnvKey = v
		case "catalog":
			p.Catalog = v
		case "small":
			p.Small = v
		case "models":
			p.Extra = strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })
		case "website":
			p.Website = v
		case "keys":
			p.KeysURL = v
		default:
			return fmt.Errorf("unknown field %q\n\n%s", k, providerUsage)
		}
	}
	return nil
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return t.Format("Jan 2")
	}
}

// refreshLive re-fetches the vendor model list of every provider that has a
// key; `dial sync` calls it after the catalog download.
func refreshLive(ctx context.Context) {
	var names []string
	for _, p := range agent.Providers() {
		if agent.Key(p.EnvKey) == "" {
			continue
		}
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		ms, err := p.RefreshModels(c)
		cancel()
		if err == nil {
			names = append(names, fmt.Sprintf("%s (%d)", p.ID, len(ms)))
		}
	}
	sort.Strings(names)
	if len(names) > 0 {
		fmt.Println(green.Render("✓"), "live model lists:", strings.Join(names, ", "))
	}
}
