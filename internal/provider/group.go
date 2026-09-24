package provider

// Routing groups: several models, from one provider or many, that an agent
// picks as one. The gateway serves a group's id like any model's and
// routes each request over every member's keys or accounts together — a
// subscription whose allowance renews soonest first across providers, one
// resting after a failure last — rather than over one provider's.
//
// The user makes groups, in the Routing view. magpie also finds some on
// its own: a model more than one provider serves under the same name is a
// group of those, derived each time and never stored until the user
// changes one. Models of different names are only ever grouped by the
// user: nothing here guesses which models are alike.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// GroupPrefix starts a group's id in the catalog: "group/<id>".
const GroupPrefix = "group/"

// Affinities are how long a conversation stays with the key or account that
// answered it: "" auto, as long as what the vendor cached of it is worth
// keeping; for the whole session; within a turn only, free to move when
// the user speaks again; or never.
var Affinities = []string{"", AffinitySession, AffinityTurn, AffinityOff}

const (
	AffinitySession = "session"
	AffinityTurn    = "turn"
	AffinityOff     = "off"
)

// Group is a routing group.
type Group struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Members  []string `json:"members"`            // "provider/model", in order
	Routing  string   `json:"routing,omitempty"`  // as Provider.Routing, over all the members' keys and accounts
	Affinity string   `json:"affinity,omitempty"` // as Provider.Affinity
	// Auto is set on a group magpie found: one model served by several
	// providers. It is derived, never stored.
	Auto bool `json:"auto,omitempty"`
	// Hidden is stored for a found group the user removed.
	Hidden bool `json:"hidden,omitempty"`
}

// Member is a group's member as it resolves now.
type Member struct {
	ID       string // as the group names it
	Provider Provider
	Model    string // what the vendor is asked for
}

// Groups lists the user's groups, then those magpie found, hidden ones
// too (marked so).
func Groups() []Group {
	return groupsIn(providerEntries())
}

func groupsIn(entries []Entry) []Group {
	f := load()
	var out []Group
	hidden := map[string]bool{}
	for _, g := range f.Groups {
		if g.Hidden {
			hidden[g.ID] = true
			continue
		}
		out = append(out, g)
	}
	for _, g := range autoGroups(entries) {
		if slices.ContainsFunc(out, func(o Group) bool { return o.ID == g.ID }) {
			continue // the user changed it: theirs now
		}
		g.Hidden = hidden[g.ID]
		out = append(out, g)
	}
	return out
}

// autoGroups are the models more than one ready provider serves under the
// same name — the vendor's own prefix aside ("anthropic/claude-sonnet-5"
// is claude-sonnet-5) — in the order the providers were added.
func autoGroups(entries []Entry) []Group {
	var order []string
	by := map[string][]Entry{}
	for _, e := range entries {
		k := strings.ToLower(e.Model)
		if i := strings.LastIndex(k, "/"); i >= 0 {
			k = k[i+1:]
		}
		if !slices.ContainsFunc(by[k], func(o Entry) bool { return o.Provider.ID == e.Provider.ID }) {
			if by[k] == nil {
				order = append(order, k)
			}
			by[k] = append(by[k], e)
		}
	}
	var out []Group
	for _, k := range order {
		es := by[k]
		if len(es) < 2 {
			continue
		}
		g := Group{ID: "auto-" + Slug(k), Name: es[0].Name, Auto: true}
		for _, e := range es {
			g.Members = append(g.Members, e.ID)
		}
		out = append(out, g)
	}
	return out
}

// FindGroup looks a group up by its catalog id ("group/<id>") and resolves
// its members; one not ready now is left out.
func FindGroup(id string) (Group, []Member, bool) {
	gid, ok := strings.CutPrefix(strings.TrimSpace(id), GroupPrefix)
	if !ok {
		return Group{}, nil, false
	}
	entries := providerEntries()
	for _, g := range groupsIn(entries) {
		if g.ID == gid && !g.Hidden {
			return g, membersIn(entries, g), true
		}
	}
	return Group{}, nil, false
}

func membersIn(entries []Entry, g Group) []Member {
	var out []Member
	seen := map[string]bool{}
	for _, id := range g.Members {
		if strings.HasPrefix(id, GroupPrefix) {
			continue // a group in a group is not one
		}
		p, m, ok := resolveIn(entries, id)
		if !ok || seen[p.ID+"/"+m] {
			continue
		}
		seen[p.ID+"/"+m] = true
		out = append(out, Member{ID: id, Provider: p, Model: m})
	}
	return out
}

// groupEntries are the catalog's groups: each with a member ready, named
// as the user named it, answering for its first member when an agent asks
// what the model can do, and offering only the reasoning levels every
// member has.
func groupEntries(entries []Entry) []Entry {
	var out []Entry
	for _, g := range groupsIn(entries) {
		if g.Hidden {
			continue
		}
		ms := membersIn(entries, g)
		if len(ms) == 0 {
			continue
		}
		e := Entry{ID: GroupPrefix + g.ID, Model: ms[0].Model, Name: g.Name, Provider: ms[0].Provider, Group: g.ID}
		for i, m := range ms {
			var efforts []string
			for _, x := range entries {
				if x.Provider.ID == m.Provider.ID && x.Model == m.Model {
					efforts = x.Efforts
				}
			}
			if i == 0 {
				e.Efforts = efforts
				continue
			}
			e.Efforts = slices.DeleteFunc(slices.Clone(e.Efforts), func(v string) bool { return !slices.Contains(efforts, v) })
		}
		out = append(out, e)
	}
	return out
}

// SaveGroup adds or replaces a group of the user's. Changing one magpie
// found makes it the user's.
func SaveGroup(g Group) error {
	g.ID = strings.ToLower(strings.TrimSpace(g.ID))
	g.Name = strings.TrimSpace(g.Name)
	if g.ID == "" {
		g.ID = Slug(g.Name)
	}
	if g.ID == "" || g.ID != Slug(g.ID) {
		return fmt.Errorf("a group's id must be lowercase letters, digits and dashes, not %q", g.ID)
	}
	if g.Name == "" {
		g.Name = g.ID
	}
	g.Members = cleanList(g.Members)
	g.Members = slices.DeleteFunc(g.Members, func(id string) bool { return strings.HasPrefix(id, GroupPrefix) })
	if len(g.Members) == 0 {
		return errors.New("a group needs a model in it")
	}
	if g.Routing != Ordered && g.Routing != Rotate && g.Routing != LeastUsed {
		g.Routing = ""
	}
	if !slices.Contains(Affinities, g.Affinity) {
		g.Affinity = ""
	}
	g.Auto, g.Hidden = false, false
	f := load()
	for i := range f.Groups {
		if f.Groups[i].ID == g.ID {
			f.Groups[i] = g
			return store(f)
		}
	}
	f.Groups = append(f.Groups, g)
	return store(f)
}

// DeleteGroup removes a group of the user's; one magpie found is hidden,
// to come back with ShowGroup.
func DeleteGroup(id string) error {
	f := load()
	found := false
	f.Groups = slices.DeleteFunc(f.Groups, func(g Group) bool {
		if g.ID == id {
			found = true
			return true
		}
		return false
	})
	if slices.ContainsFunc(autoGroups(providerEntries()), func(g Group) bool { return g.ID == id }) {
		f.Groups = append(f.Groups, Group{ID: id, Hidden: true})
		found = true
	}
	if !found {
		return fmt.Errorf("no group %q", id)
	}
	return store(f)
}

// ShowGroup brings back a group magpie found that the user had removed.
func ShowGroup(id string) error {
	f := load()
	f.Groups = slices.DeleteFunc(f.Groups, func(g Group) bool { return g.ID == id && g.Hidden })
	return store(f)
}
