package gateway

// Routing spreads a provider's requests over the keys or accounts it has
// on (see provider.Provider.Routing): smartly, in order, in turn, or the
// least used first. Whichever goes first, a key that suits the request
// still goes before one that doesn't, and one resting after a failure
// waits at the back for as long as its failure says: out of credit for
// half an hour, out of quota until it says it resets — or, for a
// subscription, until the window it filled does — rate limited until it
// says to try again, and otherwise a minute, longer each time it fails
// again.

import (
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

// usageHalfLife is how fast the tokens a key served stop counting against
// it when choosing by use: half of them after an hour.
const usageHalfLife = time.Hour

var routed = struct {
	sync.Mutex
	turn     map[string]int      // provider → requests routed in turn
	used     map[string]tokenUse // candidate rest id → tokens it served
	failures map[string]int      // candidate rest id → failures since it last answered
}{turn: map[string]int{}, used: map[string]tokenUse{}, failures: map[string]int{}}

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
	delete(routed.failures, rest)
	routed.Unlock()
}

// How long a candidate sits out, by why it failed.
const (
	creditRest   = 30 * time.Minute // out of credit: until someone tops it up
	quotaRest    = 15 * time.Minute // out of quota, with no word of when it resets
	longestWait  = time.Hour        // the most a vendor's own "try again at" is trusted
	longestRetry = 10 * time.Minute // failing again and again
)

var (
	// creditWords: the account or key has no money left.
	creditWords = regexp.MustCompile(`(?i)insufficient.?(balance|credit|fund)|balance|credit|billing|payment|arrear|overdue|suspended|余额|欠费|充值|账户.*(不足|停)`)
	// quotaWords: it has used up what its plan allows for now.
	usedUpWords = regexp.MustCompile(`(?i)quota|usage.?limit|limit.?reached|hit your .*limit|limit.{0,24}resets|exceeded.*(plan|limit)|额度|用量|套餐|上限`)
	// resetsWords: Claude Code's "usage limit reached|<when it resets>".
	resetsWords = regexp.MustCompile(`(?i)limit reached\|(\d{10})\b`)
)

// Why a candidate failed, as rest tells it.
const (
	failCredit = "credit"
	failQuota  = "quota"
	failRate   = "rate"
	failOther  = "other"
)

// failure says why a reply failed.
func failure(status int, body []byte) string {
	switch {
	case status == 402, creditWords.Match(body) && status != 429 || strings.Contains(string(body), "insufficient_quota"):
		return failCredit
	case usedUpWords.Match(body):
		return failQuota
	case status == 429:
		return failRate
	}
	return failOther
}

// restAfter sets a failed candidate aside for as long as its failure says.
func (s *Server) restAfter(c candidate, status int, header http.Header, body []byte) {
	now := time.Now()
	d := fallbackCooldown
	why := failure(status, body)
	switch why {
	case failCredit:
		d = creditRest
	case failQuota:
		d = quotaRest
		if w := retryAfter(header, now); w > 0 {
			d = w
		} else if m := resetsWords.FindSubmatch(body); m != nil {
			if n, _ := strconv.ParseInt(string(m[1]), 10, 64); time.Unix(n, 0).After(now) {
				d = time.Unix(n, 0).Sub(now)
			}
		} else if t := c.full(now); !t.IsZero() {
			d = t.Sub(now)
		}
	case failRate:
		if w := retryAfter(header, now); w > 0 {
			d = w
		}
	default:
		routed.Lock()
		routed.failures[c.rest]++
		n := routed.failures[c.rest]
		routed.Unlock()
		d = min(fallbackCooldown<<min(n-1, 10), longestRetry)
		// a subscription that failed with a window full is out of it,
		// whatever it said
		if t := c.full(now); !t.IsZero() {
			d = t.Sub(now)
		}
	}
	if a := c.p.Account; a != nil && why != failOther {
		provider.StaleAllowance(a.Agent, a.User) // ask again what it has left
	}
	restingUntil.Lock()
	restingUntil.m[c.rest] = now.Add(d)
	restingUntil.Unlock()
}

// full is when a subscription whose allowance, as last known, has a window
// used up for the candidate's model renews; zero otherwise.
func (c candidate) full(now time.Time) time.Time {
	if c.p.Account == nil {
		return time.Time{}
	}
	return allowances(c.p.Account.Agent)[c.p.Account.User].Full(c.model, usedShare, now)
}

// retryAfter is when a vendor says to try again: Retry-After, in seconds
// or as a date, or the reset headers of OpenAI's and Anthropic's kind.
func retryAfter(h http.Header, now time.Time) time.Duration {
	var d time.Duration
	if v := h.Get("Retry-After"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			d = time.Duration(n * float64(time.Second))
		} else if t, err := http.ParseTime(v); err == nil {
			d = t.Sub(now)
		}
	}
	for k, vs := range h {
		k = strings.ToLower(k)
		if d > 0 || len(vs) == 0 || !strings.Contains(k, "reset") || !strings.Contains(k, "ratelimit") {
			continue
		}
		v := vs[0]
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			d = max(d, t.Sub(now))
		} else if w, err := time.ParseDuration(v); err == nil {
			d = max(d, w)
		} else if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 1e9 {
			d = max(d, time.Unix(n, 0).Sub(now))
		}
	}
	if d <= 0 {
		return 0
	}
	return min(d, longestWait)
}

var allowances = provider.Allowances

// Shares of an allowance past which an account is kept for when the others
// can't take a request: low, and all but used up.
const (
	lowShare  = 90
	usedShare = 98
)

// route orders one provider's candidates as its routing says.
func route(p provider.Provider, cs []candidate, model string, from provider.Protocol) []candidate {
	if len(cs) < 2 {
		return cs
	}
	var known map[string]provider.Allowance
	if p.Account != nil {
		known = allowances(p.Account.Agent)
	}
	now := time.Now()
	type left struct {
		used   float64
		renews []time.Time
	}
	lefts := map[string]left{}
	for _, c := range cs {
		if c.p.Account != nil {
			u, r := known[c.p.Account.User].For(c.model, now)
			lefts[c.rest] = left{u, r}
		}
	}
	shareOf := func(c candidate) float64 { return lefts[c.rest].used }
	switch p.Routing {
	case "":
		// of those with quota to spare, the one whose allowance renews
		// soonest, since what it has left is lost then, while one renewing
		// later keeps: the biggest window decides — the week, not the five
		// hours in it — and the next one only when that renews in the
		// same hour. Those alike stay in their order, keeping the vendor's
		// prompt cache warm. Past that, whichever has the most left, and
		// one all but used up only when nothing else can take it. Only the
		// windows that count the model do: Opus's own weekly allowance
		// being used up leaves Sonnet alone.
		var fine, low, spent []candidate
		for _, c := range cs {
			switch v := shareOf(c); {
			case v >= usedShare:
				spent = append(spent, c)
			case v >= lowShare:
				low = append(low, c)
			default:
				fine = append(fine, c)
			}
		}
		sort.SliceStable(fine, func(i, j int) bool {
			ri, rj := lefts[fine[i].rest].renews, lefts[fine[j].rest].renews
			for k := 0; k < len(ri) || k < len(rj); k++ {
				var a, b time.Time // to the hour, so a few minutes don't reorder
				if k < len(ri) {
					a = ri[k].Truncate(time.Hour)
				}
				if k < len(rj) {
					b = rj[k].Truncate(time.Hour)
				}
				switch {
				case a.Equal(b):
					continue
				case a.IsZero() || b.IsZero(): // not known: after those known
					return b.IsZero()
				}
				return a.Before(b)
			}
			return false
		})
		for _, l := range [][]candidate{low, spent} {
			sort.SliceStable(l, func(i, j int) bool {
				return shareOf(l[i]) < shareOf(l[j])
			})
		}
		cs = append(append(fine, low...), spent...)
	case provider.Ordered:
		return cs
	case provider.Rotate:
		routed.Lock()
		n := routed.turn[p.ID] % len(cs)
		routed.turn[p.ID]++
		routed.Unlock()
		cs = append(append([]candidate{}, cs[n:]...), cs[:n]...)
	case provider.LeastUsed:
		// a subscription by the share of its allowance used, as the vendor
		// says; then, and for keys, by what magpie sent it lately
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
			if sa, sb := shareOf(ca), shareOf(cb); sa != sb {
				return sa < sb
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
