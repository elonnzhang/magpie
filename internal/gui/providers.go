package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/yetone/dial/internal/agent"
	"github.com/yetone/dial/internal/catalog"
)

// The providers page: everything dial knows about a vendor, plus which
// agents can use it and which do right now.

type providerJSON struct {
	agent.Provider
	Host  string   `json:"host"`
	Extra []string `json:"extra"` // the user's own model ids (Provider.Extra is shadowed by Models below)
	Key   struct {
		State  string `json:"state"` // "env", "stored" or ""
		Masked string `json:"masked"`
	} `json:"key"`
	Models struct {
		Count   int    `json:"count"`
		Live    bool   `json:"live"`    // the list came from the vendor itself
		Fetched string `json:"fetched"` // when, as "3h ago"
	} `json:"models"`
	Agents []providerAgent `json:"agents"`
}

type providerAgent struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Icon    string `json:"icon"`
	Current bool   `json:"current"` // this agent is on this provider now
}

type providersJSON struct {
	Providers []providerJSON   `json:"providers"`
	Hidden    []agent.Provider `json:"hidden"`
	Catalogs  []string         `json:"catalogs"` // models.dev provider ids, for the catalog field
}

func providerInfo(p agent.Provider, agents []*agent.Agent) providerJSON {
	out := providerJSON{Provider: p, Host: p.Host(), Extra: p.Extra, Agents: []providerAgent{}}
	if out.Extra == nil {
		out.Extra = []string{}
	}
	out.Key.State, out.Key.Masked = agent.KeyState(p.EnvKey)
	out.Models.Count = len(p.Models())
	if n, t, ok := p.LiveModels(); ok {
		out.Models.Live, out.Models.Count, out.Models.Fetched = true, n, ago(t)
	}
	for _, a := range agents {
		f := a.Field("provider")
		if f == nil {
			continue
		}
		cur := f.Get()
		for _, o := range f.Options(nil) {
			if o.Value == p.ID {
				out.Agents = append(out.Agents, providerAgent{ID: a.ID, Name: a.Name, Icon: a.Icon, Current: cur == p.ID})
				break
			}
		}
	}
	return out
}

func providersState() providersJSON {
	agents := agent.Detected()
	s := providersJSON{Providers: []providerJSON{}, Hidden: agent.HiddenProviders(), Catalogs: catalog.Providers()}
	if s.Hidden == nil {
		s.Hidden = []agent.Provider{}
	}
	for _, p := range agent.Providers() {
		s.Providers = append(s.Providers, providerInfo(p, agents))
	}
	return s
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strings.TrimSuffix(d.Round(time.Minute).String(), "0s") + " ago"
	case d < 48*time.Hour:
		return d.Round(time.Hour).String() + " ago"
	default:
		return t.Format("Jan 2")
	}
}

func providerRoutes(mux *http.ServeMux, w Windows) {
	mux.HandleFunc("GET /api/providers", func(rw http.ResponseWriter, r *http.Request) {
		writeJSON(rw, providersState())
	})
	mux.HandleFunc("POST /api/provider/{action}", func(rw http.ResponseWriter, r *http.Request) {
		var in struct {
			agent.Provider
			Key string `json:"key"` // API key to store alongside a save, if given
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(rw, err)
			return
		}
		var err error
		switch r.PathValue("action") {
		case "save":
			if err = agent.SaveProvider(in.Provider); err == nil && in.Key != "" {
				if p, e := agent.FindProvider(in.ID); e == nil {
					err = agent.SetKey(p.EnvKey, in.Key)
				}
			}
		case "delete":
			err = agent.DeleteProvider(in.ID)
		case "reset":
			err = agent.ResetProvider(in.ID)
		case "test":
			p, e := agent.FindProvider(in.ID)
			if e != nil {
				fail(rw, e)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			writeJSON(rw, struct {
				Results  []agent.TestResult `json:"results"`
				Provider providerJSON       `json:"provider"`
			}{p.Test(ctx), providerInfo(*p, agent.Detected())})
			return
		case "models":
			p, e := agent.FindProvider(in.ID)
			if e != nil {
				fail(rw, e)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			if _, err = p.RefreshModels(ctx); err != nil {
				fail(rw, err)
				return
			}
			writeJSON(rw, struct {
				Models   []catalog.Model `json:"models"`
				Provider providerJSON    `json:"provider"`
			}{p.Models(), providerInfo(*p, agent.Detected())})
			return
		default:
			http.NotFound(rw, r)
			return
		}
		if err != nil {
			fail(rw, err)
			return
		}
		writeJSON(rw, providersState())
	})
	mux.HandleFunc("POST /api/key", func(rw http.ResponseWriter, r *http.Request) {
		var in struct{ Env, Value string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(rw, err)
			return
		}
		if err := agent.SetKey(in.Env, in.Value); err != nil {
			fail(rw, err)
			return
		}
		writeJSON(rw, providersState())
	})
	mux.HandleFunc("POST /api/open", func(rw http.ResponseWriter, r *http.Request) {
		var in struct{ URL string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if strings.HasPrefix(in.URL, "https://") || strings.HasPrefix(in.URL, "http://") {
			w.OpenURL(in.URL)
		}
		rw.WriteHeader(http.StatusNoContent)
	})
}
