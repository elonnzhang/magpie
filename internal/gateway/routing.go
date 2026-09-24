package gateway

// Routing spreads a provider's requests over the keys or accounts it has
// on (see provider.Provider.Routing): in turn, or the least used first.
// Whichever goes first, a key that suits the request still goes before one
// that doesn't, and one resting after a failure still waits at the back.

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

// usageHalfLife is how fast the tokens a key served stop counting against
// it when choosing by use: half of them after an hour.
const usageHalfLife = time.Hour

var routed = struct {
	sync.Mutex
	turn map[string]int      // provider → requests routed in turn
	used map[string]tokenUse // candidate rest id → tokens it served
}{turn: map[string]int{}, used: map[string]tokenUse{}}

type tokenUse struct {
	n  float64
	at time.Time
}

func (u tokenUse) now(t time.Time) float64 {
	return u.n * math.Exp2(-t.Sub(u.at).Seconds()/usageHalfLife.Seconds())
}

// served counts what a candidate just answered against it.
func served(rest string, tokens int) {
	if tokens <= 0 {
		tokens = 1 // it answered, whether or not it said how much
	}
	now := time.Now()
	routed.Lock()
	routed.used[rest] = tokenUse{routed.used[rest].now(now) + float64(tokens), now}
	routed.Unlock()
}

// route orders one provider's candidates as its routing says.
func route(p provider.Provider, cs []candidate, model string, from provider.Protocol) []candidate {
	if len(cs) < 2 {
		return cs
	}
	switch p.Routing {
	case provider.Rotate:
		routed.Lock()
		n := routed.turn[p.ID] % len(cs)
		routed.turn[p.ID]++
		routed.Unlock()
		cs = append(append([]candidate{}, cs[n:]...), cs[:n]...)
	case provider.LeastUsed:
		// a subscription by the share of its allowance used, as the vendor
		// says; then, and for keys, by what magpie sent it lately
		var share map[string]float64
		if p.Account != nil {
			share = provider.AllowanceUsed(p.Account.Agent)
		}
		now := time.Now()
		routed.Lock()
		tokens := make([]float64, len(cs))
		for i, c := range cs {
			tokens[i] = routed.used[c.rest].now(now)
		}
		routed.Unlock()
		idx := make([]int, len(cs))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool {
			ca, cb := cs[idx[a]], cs[idx[b]]
			if ca.p.Account != nil && cb.p.Account != nil {
				if sa, sb := share[ca.p.Account.User], share[cb.p.Account.User]; sa != sb {
					return sa < sb
				}
			}
			return tokens[idx[a]] < tokens[idx[b]]
		})
		out := make([]candidate, len(cs))
		for i, j := range idx {
			out[i] = cs[j]
		}
		cs = out
	default:
		return cs
	}
	if p.Account == nil {
		sort.SliceStable(cs, func(i, j int) bool { return keyFit(cs[i].p, model, from) < keyFit(cs[j].p, model, from) })
	}
	return cs
}
