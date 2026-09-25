package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/yetone/magpie/internal/provider"
)

// A group's rules from the terminal: magpie group rule add|rm|mv …, as the
// Routing view's Rules section makes them.

const ruleUsage = `usage:
  magpie group rule <group>               the group's rules
  magpie group rule add <group> use=<model> [tokens=<n>] [images] [effort=on|low|medium|high|xhigh|max] [agents=a,b…] [at=<n>]
                                          a rule: a turn that matches it goes to <model>, one of the group's, first
  magpie group rule rm <group> <n>        remove rule n
  magpie group rule mv <group> <n> <to>   move rule n to place <to>

  Rules are looked at top first when you send a message (a new turn); the first that
  matches puts its model first, and the group's others stay behind it if it fails.
  The agent's tool results within the turn stay with the model the turn began on.
  Every condition given must hold:
  tokens   the request is at least this long (200000, 200k, 1m): estimated from its size, or
           what the vendor counted the conversation's last request as, whichever is more
  images   it carries an image, now or earlier in the conversation
  effort   the agent asked for reasoning: on (any), or at least this level
  agents   it comes from one of these agents (claude, codex, opencode, … as magpie usage names them)

  e.g. magpie group rule add opus-anywhere use=openrouter/google/gemini-3-pro tokens=200k
       magpie group rule add opus-anywhere use=a/vision-model images`

// parseTokens reads 200000, 200k, 1.5m.
func parseTokens(v string) (int, error) {
	s := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(v, "_", "")))
	mul := 1.0
	switch {
	case strings.HasSuffix(s, "k"):
		mul, s = 1e3, strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mul, s = 1e6, strings.TrimSuffix(s, "m")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("tokens %q is not a length (200000, 200k, 1m)", v)
	}
	return int(f * mul), nil
}

// parseRule makes a rule of k=v words; the model is resolved among the
// group's members.
func parseRule(g provider.Group, words []string) (provider.Rule, int, error) {
	var r provider.Rule
	at := 0
	for _, w := range words {
		k, v, hasV := strings.Cut(w, "=")
		k = strings.ToLower(strings.TrimSpace(k))
		switch k {
		case "use", "model", "to":
			id, err := groupMember(g, v)
			if err != nil {
				return r, 0, err
			}
			r.Use = id
		case "tokens", "context", "longer":
			n, err := parseTokens(v)
			if err != nil {
				return r, 0, err
			}
			r.Tokens = n
		case "images", "image":
			switch strings.ToLower(v) {
			case "", "yes", "true", "on", "1":
				r.Images = true
			case "no", "false", "off", "0":
				r.Images = false
			default:
				return r, 0, fmt.Errorf("images takes no value (or yes/no), not %q", v)
			}
		case "effort", "reasoning", "thinking":
			if !hasV {
				v = "on"
			}
			r.Effort = strings.ToLower(strings.TrimSpace(v))
		case "agents", "agent":
			r.Agents = splitList(v)
		case "at":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return r, 0, fmt.Errorf("at is a place from 1, not %q", v)
			}
			at = n
		default:
			return r, 0, fmt.Errorf("unknown %q (use, tokens, images, effort, agents, at)\n\n%s", w, ruleUsage)
		}
	}
	if r.Use == "" {
		return r, 0, fmt.Errorf("use=<model> is missing: one of %s", strings.Join(g.Members, ", "))
	}
	return r, at, nil
}

// groupMember is the group's member a typed model names: its id, its
// model id without the provider, or the model's last name.
func groupMember(g provider.Group, in string) (string, error) {
	in = strings.TrimPrefix(strings.TrimSpace(in), "magpie/")
	for _, match := range []func(string) bool{
		func(m string) bool { return m == in },
		func(m string) bool { return strings.EqualFold(m, in) },
		func(m string) bool { _, bare, _ := strings.Cut(m, "/"); return strings.EqualFold(bare, in) },
		func(m string) bool { return strings.EqualFold(m[strings.LastIndex(m, "/")+1:], in) },
	} {
		var hits []string
		for _, m := range g.Members {
			if match(m) {
				hits = append(hits, m)
			}
		}
		switch len(hits) {
		case 0:
			continue
		case 1:
			return hits[0], nil
		default:
			return "", fmt.Errorf("%s is %s: name one", in, strings.Join(hits, " and "))
		}
	}
	return "", fmt.Errorf("%s is not in %s (its models: %s)", in, g.ID, strings.Join(g.Members, ", "))
}

// ruleCmd: magpie group rule …
func ruleCmd(args []string) error {
	if len(args) == 0 || slices.Contains([]string{"help", "-h", "--help"}, args[0]) {
		fmt.Println(ruleUsage)
		return nil
	}
	verb, rest := args[0], args[1:]
	if len(rest) == 0 {
		// magpie group rule <group>: the group, its rules with it
		if g, err := findGroup(verb); err == nil {
			if len(g.Rules) == 0 {
				fmt.Println(muted.Render("  " + g.ID + " has no rules · magpie group rule add " + g.ID + " use=<model> …"))
				return nil
			}
			return showGroup(g)
		}
		return fmt.Errorf("%s", ruleUsage)
	}
	g, err := findGroup(rest[0])
	if err != nil {
		return err
	}
	if g.Hidden {
		return fmt.Errorf("%s was removed: magpie group restore %s brings it back first", g.ID, g.ID)
	}
	place := func(s string) (int, error) {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > len(g.Rules) {
			if len(g.Rules) == 0 {
				return 0, fmt.Errorf("%s has no rules", g.ID)
			}
			return 0, fmt.Errorf("rule %s: %s has rules 1 to %d", s, g.ID, len(g.Rules))
		}
		return n - 1, nil
	}
	switch verb {
	case "add", "new":
		r, at, err := parseRule(g, rest[1:])
		if err != nil {
			return err
		}
		if at == 0 || at > len(g.Rules) {
			g.Rules = append(g.Rules, r)
		} else {
			g.Rules = slices.Insert(g.Rules, at-1, r)
		}
	case "rm", "remove", "delete":
		if len(rest) != 2 {
			return fmt.Errorf("magpie group rule rm <group> <n>")
		}
		i, err := place(rest[1])
		if err != nil {
			return err
		}
		g.Rules = slices.Delete(g.Rules, i, i+1)
	case "mv", "move":
		if len(rest) != 3 {
			return fmt.Errorf("magpie group rule mv <group> <n> <to>")
		}
		i, err := place(rest[1])
		if err != nil {
			return err
		}
		j, err := place(rest[2])
		if err != nil {
			return err
		}
		r := g.Rules[i]
		g.Rules = slices.Insert(slices.Delete(g.Rules, i, i+1), j, r)
	default:
		return fmt.Errorf("magpie group rule has no %q\n\n%s", verb, ruleUsage)
	}
	if err := provider.SaveGroup(g); err != nil {
		return err
	}
	g, err = findGroup(g.ID)
	if err != nil {
		return err
	}
	fmt.Println(green.Render("✓"), "saved", bold.Render(g.Name))
	return showGroup(g)
}

// ruleLine is a rule as magpie group shows it.
func ruleLine(r provider.Rule) string {
	return strings.Join(r.Conditions(), " · ") + muted.Render(" → ") + r.Use
}

// pruneRules drops the rules for models no longer in the group, saying so.
func pruneRules(g *provider.Group) {
	var keep []provider.Rule
	for i, r := range g.Rules {
		if slices.Contains(g.Members, r.Use) {
			keep = append(keep, r)
		} else {
			fmt.Println(amber.Render("!"), fmt.Sprintf("rule %d is removed with %s", i+1, r.Use))
		}
	}
	g.Rules = keep
}
