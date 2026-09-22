// dial — one small dial for every coding agent's model.
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
	"github.com/yetone/dial/internal/catalog"
	"github.com/yetone/dial/internal/profile"
	"github.com/yetone/dial/internal/tui"
)

var version = "dev"

const usage = `dial — one dial for every coding agent's model

  dial                          open the app: a window plus a menu bar icon
  dial tray                     start in the menu bar only
  dial tui                      the same dial, in the terminal
  dial ls                       list detected agents and their settings
  dial <agent>                  show one agent
  dial <agent> <model>          set an agent's model
  dial <agent> <field> <value>  set another field   e.g. dial codex effort high
                                                    e.g. dial claude provider deepseek

  dial save <name>              snapshot every agent's settings as a profile
  dial use <name>               apply a profile
  dial profiles                 list profiles
  dial rm <name>                delete a profile

  dial providers                list providers: endpoints, keys, who uses them
  dial provider <id>            show one provider and its models
  dial provider add <name> k=v… add a provider   (dial provider for the fields)
  dial provider edit|rm|reset|test|models <id>

  dial key <VAR> <value>        store an API key for apps that see no shell env
  dial keys                     list stored keys (values masked)
  dial sync                     refresh the model catalog and live model lists
  dial agents                   list every supported agent

agents: claude (cc), codex, gemini, opencode (oc), pi, goose, cursor, copilot, crush
`

var (
	bold  = lipgloss.NewStyle().Bold(true)
	muted = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#8B8F98", Dark: "#7C8290"})
	faint = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#C4C7CE", Dark: "#4A4F5A"})
	green = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#0F9D58", Dark: "#7EE2A8"})
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		if hasGUI {
			return runGUI(true)
		}
		return tui.Run()
	}
	switch args[0] {
	case "tui":
		return tui.Run()
	case "app", "gui":
		return runGUI(true)
	case "tray":
		return runGUI(false)
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	case "-v", "--version", "version":
		fmt.Println("dial", version)
		return nil
	case "ls", "list":
		return list(agent.Detected(), true)
	case "agents":
		return list(agent.All(), false)
	case "sync":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := catalog.Sync(ctx); err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "catalog saved to", catalog.CachePath())
		refreshLive(ctx)
		return nil
	case "save", "use", "rm", "profiles":
		return profiles(args)
	case "providers":
		return providers()
	case "provider":
		return provider(args)
	case "key", "keys":
		return keys(args)
	}

	a, err := agent.Find(args[0])
	if err != nil {
		return err
	}
	switch len(args) {
	case 1:
		return list([]*agent.Agent{a}, true)
	case 2:
		// `dial codex xhigh`: a bare value that belongs to a non-model field
		// (effort levels, for instance) is routed there; anything else is a model.
		if f := fieldForValue(a, args[1]); f != nil {
			return set(a, f.Key, args[1])
		}
		return set(a, a.Fields[0].Key, args[1])
	case 3:
		return set(a, args[1], args[2])
	}
	return fmt.Errorf("too many arguments\n\n%s", usage)
}

func set(a *agent.Agent, key, value string) error {
	f := a.Field(key)
	if f == nil {
		var keys []string
		for _, f := range a.Fields {
			keys = append(keys, f.Key)
		}
		return fmt.Errorf("%s has no field %q (fields: %s)", a.Name, key, strings.Join(keys, ", "))
	}
	if err := f.Set(value); err != nil {
		return err
	}
	fmt.Println(green.Render("✓"), bold.Render(a.Name), muted.Render(f.Label), value)
	if a.Notice != nil {
		if n := a.Notice(); n != "" {
			fmt.Println(muted.Render("  ↻ " + n))
		}
	}
	return nil
}

// keys: `dial keys`, `dial key VAR value`, `dial key VAR` (show), `dial key VAR -` (forget).
func keys(args []string) error {
	stored := agent.StoredKeys()
	switch len(args) {
	case 1:
		if len(stored) == 0 {
			fmt.Println(muted.Render("no stored keys —"), "dial key DEEPSEEK_API_KEY sk-…")
			return nil
		}
		names := make([]string, 0, len(stored))
		for k := range stored {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			fmt.Println(" ", pad(k, 24), muted.Render(agent.Mask(stored[k])))
		}
		fmt.Println(faint.Render("  " + tilde(agent.KeysPath())))
		return nil
	case 2:
		if v, ok := stored[args[1]]; ok {
			fmt.Println(agent.Mask(v))
			return nil
		}
		if os.Getenv(args[1]) != "" {
			fmt.Println(muted.Render("set in the environment, not stored"))
			return nil
		}
		return fmt.Errorf("%s is not set", args[1])
	case 3:
		v := args[2]
		if v == "-" {
			v = ""
		}
		if err := agent.SetKey(args[1], v); err != nil {
			return err
		}
		if v == "" {
			fmt.Println(green.Render("✓"), "forgot", args[1])
		} else {
			fmt.Println(green.Render("✓"), "stored", args[1], muted.Render(agent.Mask(v)))
		}
		return nil
	}
	return fmt.Errorf("usage: dial key <VAR> <value>")
}

func fieldForValue(a *agent.Agent, v string) *agent.Field {
	vals := a.Values()
	for i := 1; i < len(a.Fields); i++ {
		for _, o := range a.Fields[i].Options(vals) {
			if o.Value == v {
				return &a.Fields[i]
			}
		}
	}
	return nil
}

func list(agents []*agent.Agent, detectedOnly bool) error {
	if len(agents) == 0 {
		return fmt.Errorf("no supported agents found on this machine")
	}
	type row struct{ name, vals, path string }
	var rows []row
	nameW, valW := 0, 0
	for _, a := range agents {
		r := row{name: a.Name, path: tilde(a.Path)}
		if !detectedOnly && !a.Detected() {
			r.name = faint.Render(a.Name)
			r.vals = faint.Render("not detected")
			r.path = ""
		} else {
			vals := a.Values()
			var parts []string
			for _, f := range a.Fields {
				v := vals[f.Key]
				if v == "" {
					v = faint.Render("—")
				}
				if f.Label == "model" {
					parts = append(parts, v)
				} else {
					parts = append(parts, muted.Render(f.Label)+" "+v)
				}
			}
			r.name = bold.Render(a.Name)
			r.vals = strings.Join(parts, muted.Render("  ·  "))
		}
		nameW = max(nameW, lipgloss.Width(r.name))
		valW = max(valW, lipgloss.Width(r.vals))
		rows = append(rows, r)
	}
	for _, r := range rows {
		fmt.Printf("  %s  %s  %s\n", pad(r.name, nameW), pad(r.vals, valW), faint.Render(r.path))
	}
	return nil
}

func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

func profiles(args []string) error {
	switch args[0] {
	case "profiles":
		ps, err := profile.Load()
		if err != nil {
			return err
		}
		if len(ps) == 0 {
			fmt.Println(muted.Render("no profiles yet · dial save <name>"))
			return nil
		}
		for _, n := range profile.Names(ps) {
			fmt.Printf("  %s  %s\n", bold.Render(n), muted.Render(profile.Summary(ps[n])))
		}
		return nil
	case "save":
		if len(args) < 2 {
			return fmt.Errorf("usage: dial save <name>")
		}
		if err := profile.Save(args[1], profile.Snapshot()); err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "saved profile", bold.Render(args[1]))
		return nil
	case "use":
		if len(args) < 2 {
			return fmt.Errorf("usage: dial use <name>")
		}
		ps, err := profile.Load()
		if err != nil {
			return err
		}
		p, ok := ps[args[1]]
		if !ok {
			return fmt.Errorf("no profile named %q", args[1])
		}
		n, err := profile.Apply(p)
		if err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "applied", bold.Render(args[1]), muted.Render(fmt.Sprintf("(%d changed)", n)))
		return nil
	case "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: dial rm <name>")
		}
		if err := profile.Delete(args[1]); err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "deleted profile", bold.Render(args[1]))
		return nil
	}
	return nil
}

func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}
