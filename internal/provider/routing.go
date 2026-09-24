package provider

// Routing: how the gateway spreads requests over the keys or accounts a
// provider has on (see Provider.Routing). Choosing by use needs to know the
// use without waiting for it, so a subscription's allowance is read from
// what was fetched last, and fetched again behind the request when it has
// gone stale.

import (
	"context"
	"sync"
	"time"
)

// The routings besides the default, smart, in order.
const (
	Ordered   = "order"
	Rotate    = "rotate"
	LeastUsed = "usage"
)

// SetRouting changes how a provider's requests spread over its keys or
// accounts.
func SetRouting(id, routing string) error {
	p, err := Find(id)
	if err != nil {
		return err
	}
	p.Routing = routing
	return Save(*p)
}

var usedCache struct {
	sync.Mutex
	m       map[string]map[string]Allowance // agent → user → allowance
	at      map[string]time.Time
	loading map[string]bool
}

// Allowance is how much of its allowance an account has used and when it
// renews.
type Allowance struct {
	Used   float64   // share of the fullest window, 0–100
	Resets time.Time // when the last of its windows resets; zero when not known
}

// Allowances is each of an agent's accounts' allowance by user, as last
// known: it never waits, and asks again in the background when that was
// over a minute ago. An account missing is one not known yet.
func Allowances(agent string) map[string]Allowance {
	c := &usedCache
	c.Lock()
	defer c.Unlock()
	if c.m == nil {
		c.m, c.at, c.loading = map[string]map[string]Allowance{}, map[string]time.Time{}, map[string]bool{}
	}
	if !c.loading[agent] && time.Since(c.at[agent]) > time.Minute {
		c.loading[agent] = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			all := map[string]Allowance{}
			for user, q := range LoginUsage(ctx, agent) {
				if q.Error != "" || len(q.Windows) == 0 {
					continue
				}
				all[user] = allowanceOf(q.Windows, time.Now())
			}
			c.Lock()
			c.m[agent], c.at[agent], c.loading[agent] = all, time.Now(), false
			c.Unlock()
		}()
	}
	return c.m[agent]
}

// allowanceOf sums an account's windows up: the fullest decides how much
// is used, the one resetting last when the whole allowance is new again
// (a week's, say, not the five hours' within it).
func allowanceOf(ws []QuotaWindow, now time.Time) Allowance {
	var a Allowance
	for _, w := range ws {
		a.Used = max(a.Used, w.Used)
		t := time.Time{}
		switch {
		case w.ResetsAt != nil:
			t = *w.ResetsAt
		case w.ResetSecs > 0:
			t = now.Add(time.Duration(w.ResetSecs) * time.Second)
		}
		if t.After(a.Resets) {
			a.Resets = t
		}
	}
	return a
}
