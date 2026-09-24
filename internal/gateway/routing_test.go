package gateway

import (
	"net/http"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

// Tests don't ask vendors how much of an allowance is used: the gateway
// would, behind a request, of whatever upstream a test serves.
func init() { allowances = func(string) map[string]provider.Allowance { return nil } }

func restsOf(cs []candidate) string {
	s := ""
	for _, c := range cs {
		s += c.rest + " "
	}
	return s
}

func TestRouting(t *testing.T) {
	cs := []candidate{{rest: "r#a"}, {rest: "r#b"}, {rest: "r#c"}}
	p := provider.Provider{ID: "r"}
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "r#a r#b r#c " {
		t.Fatalf("smart: %s", got)
	}

	p.Routing = provider.Rotate
	var firsts string
	for range 4 {
		firsts += route(p, cs, "m", provider.Chat)[0].rest + " "
	}
	if firsts != "r#a r#b r#c r#a " {
		t.Fatalf("in turn: %s", firsts)
	}
	if restsOf(cs) != "r#a r#b r#c " {
		t.Fatal("rotating changed the list it was given")
	}

	p.Routing = provider.Ordered
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "r#a r#b r#c " {
		t.Fatalf("in order: %s", got)
	}

	p.ID, p.Routing = "u", provider.LeastUsed
	cs = []candidate{{rest: "u#a"}, {rest: "u#b"}, {rest: "u#c"}}
	served("u#a", 5000)
	served("u#c", 10)
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "u#b u#c u#a " {
		t.Fatalf("least used: %s", got)
	}
}

// Smart routing keeps the first while it has quota to spare, then goes to
// whichever has the most; a failure rests as long as it says.
func TestSmartRouting(t *testing.T) {
	old := allowances
	defer func() { allowances = old }()
	share := map[string]provider.Allowance{"a": {Used: 10}, "b": {Used: 50}, "c": {Used: 20}}
	allowances = func(string) map[string]provider.Allowance { return share }
	acct := func(user string) candidate {
		return candidate{p: provider.Provider{Account: &provider.Account{Agent: "x", User: user}}, rest: "s#" + user}
	}
	p := provider.Provider{ID: "s", Account: &provider.Account{Agent: "x"}}
	cs := []candidate{acct("a"), acct("b"), acct("c")}
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "s#a s#b s#c " {
		t.Fatalf("all fine: %s", got)
	}
	share["a"], share["b"] = provider.Allowance{Used: 99}, provider.Allowance{Used: 93}
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "s#c s#b s#a " {
		t.Fatalf("a used up, b low: %s", got)
	}

	// the allowance renewing soonest goes first: what it has left is lost
	// at its reset; one not known, or already renewed, after
	now := time.Now()
	share = map[string]provider.Allowance{
		"a": {Used: 5, Resets: now.Add(6 * 24 * time.Hour)},
		"b": {Used: 70, Resets: now.Add(20 * time.Hour)},
		"c": {Used: 10},
		"d": {Used: 40, Resets: now.Add(3 * 24 * time.Hour)},
		"e": {Used: 95, Resets: now.Add(time.Hour)},
		"f": {Used: 1, Resets: now.Add(-time.Hour)},
	}
	cs = []candidate{acct("a"), acct("b"), acct("c"), acct("d"), acct("e"), acct("f")}
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "s#b s#d s#a s#c s#f s#e " {
		t.Fatalf("by reset: %s", got)
	}
	// minutes apart is the same hour: the order given stays
	share["a"] = provider.Allowance{Used: 5, Resets: share["b"].Resets.Truncate(time.Hour).Add(time.Minute)}
	share["b"] = provider.Allowance{Used: 70, Resets: share["b"].Resets.Truncate(time.Hour).Add(2 * time.Minute)}
	if got := restsOf(route(p, cs, "m", provider.Chat)[:2]); got != "s#a s#b " {
		t.Fatalf("same hour: %s", got)
	}

	for _, c := range []struct {
		status int
		body   string
		want   string
	}{
		{402, `{}`, failCredit},
		{400, `{"error":{"message":"Your credit balance is too low"}}`, failCredit},
		{429, `{"error":{"code":"insufficient_quota"}}`, failCredit},
		{403, `账户余额不足`, failCredit},
		{429, `{"error":{"message":"usage limit reached for your plan"}}`, failQuota},
		{429, `{"error":{"message":"Too many requests"}}`, failRate},
		{503, `overloaded`, failOther},
	} {
		if got := failure(c.status, []byte(c.body)); got != c.want {
			t.Errorf("%d %s: %s, want %s", c.status, c.body, got, c.want)
		}
	}

	s := &Server{}
	until := func(id string) time.Duration {
		restingUntil.Lock()
		defer restingUntil.Unlock()
		return time.Until(restingUntil.m[id]).Round(time.Minute)
	}
	s.restAfter("t#credit", 402, http.Header{}, nil)
	h := http.Header{}
	h.Set("Retry-After", "300")
	s.restAfter("t#rate", 429, h, []byte("slow down"))
	s.restAfter("t#fail", 500, http.Header{}, nil)
	s.restAfter("t#fail", 500, http.Header{}, nil)
	if until("t#credit") != creditRest || until("t#rate") != 5*time.Minute || until("t#fail") != 2*time.Minute {
		t.Fatalf("rests: %v %v %v", until("t#credit"), until("t#rate"), until("t#fail"))
	}
	served("t#fail", 1)
	s.restAfter("t#fail", 500, http.Header{}, nil)
	if until("t#fail") != time.Minute {
		t.Fatalf("answering again starts over: %v", until("t#fail"))
	}
	h = http.Header{}
	h.Set("X-Ratelimit-Reset-Tokens", "6m0s")
	if d := retryAfter(h, time.Now()); d != 6*time.Minute {
		t.Fatalf("reset header: %v", d)
	}
}
