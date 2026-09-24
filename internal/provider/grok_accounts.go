package provider

// Several Grok subscriptions. The Grok CLI keeps one account, in its own
// home (~/.grok), which magpie only ever reads. Each further account gets a
// home of magpie's own, signed in there by the CLI's own `login`, so the
// user's sign-in is never touched and each refresh token has one holder.
// logins.json names those homes; the CLI's own account is remembered there
// too, with no home, so it can stand behind another or be turned off.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// grokAccountsDir holds the homes of the Grok accounts magpie signed in.
func grokAccountsDir() string { return filepath.Join(filepath.Dir(Path()), "grok") }

// grokLogin is a Grok account and the home it is signed in in.
type grokLogin struct {
	Login
	Home string
}

// grokLogins lists the Grok accounts that are signed in, the first in use
// first, then the rest as they were added.
func grokLogins() []grokLogin {
	loginsMu.Lock()
	defer loginsMu.Unlock()
	ls := readLogins()
	// the CLI's own account, as it is now
	if c, ok := readGrokCredential(GrokHome()); ok && c.Email != "" {
		found := false
		for i := range ls {
			if ls[i].Agent == "grok" && ls[i].Home == "" {
				found = true
				if !strings.EqualFold(ls[i].User, c.Email) {
					ls[i].User, ls[i].Seen = c.Email, time.Now().UTC().Truncate(time.Second)
					_ = writeLogins(ls)
				}
			}
		}
		if !found {
			ls = append(ls, savedLogin{Agent: "grok", User: c.Email, Seen: time.Now().UTC().Truncate(time.Second)})
			_ = writeLogins(ls)
		}
	}
	var out []grokLogin
	first := -1
	for _, l := range ls {
		if l.Agent != "grok" {
			continue
		}
		home := l.Home
		if home == "" {
			home = GrokHome()
		}
		if _, ok := readGrokCredential(home); !ok {
			continue // signed out there
		}
		g := grokLogin{Login: Login{Agent: "grok", User: l.User, Plan: l.Plan, Seen: l.Seen, On: l.On}, Home: home}
		if l.First || (first < 0 && l.Home == "") {
			first = len(out)
		}
		out = append(out, g)
	}
	if len(out) == 0 {
		return nil
	}
	if first < 0 {
		first = 0
	}
	out[first].Active, out[first].On = true, true
	return append([]grokLogin{out[first]}, append(out[:first:first], out[first+1:]...)...)
}

// grokLoginList is the Grok accounts as Logins lists them.
func grokLoginList() []Login {
	var out []Login
	for _, g := range grokLogins() {
		out = append(out, g.Login)
	}
	return out
}

// editGrokLogin changes the saved Grok account of user.
func editGrokLogin(user string, f func(ls []savedLogin, i int) ([]savedLogin, error)) error {
	loginsMu.Lock()
	defer loginsMu.Unlock()
	ls := readLogins()
	for i := range ls {
		if ls[i].Agent == "grok" && strings.EqualFold(ls[i].User, user) {
			ls, err := f(ls, i)
			if err != nil {
				return err
			}
			return writeLogins(ls)
		}
	}
	return fmt.Errorf("no Grok account %q", user)
}

func grokActive(user string) bool {
	for _, g := range grokLogins() {
		if g.Active {
			return strings.EqualFold(g.User, user)
		}
	}
	return false
}

// switchGrokLogin puts a Grok account first. The CLI's own sign-in stays
// as it is: magpie only changes which account its gateway uses first.
func switchGrokLogin(user string) error {
	var was string
	for _, g := range grokLogins() {
		if g.Active {
			was = g.User
		}
	}
	return editGrokLogin(user, func(ls []savedLogin, i int) ([]savedLogin, error) {
		for j := range ls {
			if ls[j].Agent == "grok" {
				if j != i && strings.EqualFold(ls[j].User, was) {
					ls[j].On = true // the one it replaces is next in line
				}
				ls[j].First = j == i
			}
		}
		return ls, nil
	})
}

func setGrokLoginOn(user string, on bool) error {
	if !on && grokActive(user) {
		return fmt.Errorf("magpie uses %s first; put another Grok account first to stop using it", user)
	}
	return editGrokLogin(user, func(ls []savedLogin, i int) ([]savedLogin, error) {
		ls[i].On = on
		return ls, nil
	})
}

// forgetGrokLogin drops an account magpie signed in, with its home. The
// CLI's own is signed out in the CLI.
func forgetGrokLogin(user string) error {
	if grokActive(user) {
		return fmt.Errorf("magpie uses %s first; put another Grok account first", user)
	}
	var home string
	err := editGrokLogin(user, func(ls []savedLogin, i int) ([]savedLogin, error) {
		if ls[i].Home == "" {
			return nil, errors.New("that is the Grok CLI's own sign-in; run `grok logout` to sign it out")
		}
		home = ls[i].Home
		return append(ls[:i], ls[i+1:]...), nil
	})
	if err == nil {
		removeGrokHome(home)
	}
	return err
}

// removeGrokHome deletes a home magpie made, and nothing else.
func removeGrokHome(home string) {
	if rel, err := filepath.Rel(grokAccountsDir(), home); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		_ = os.RemoveAll(home)
	}
}

// newGrokHome makes the home a further Grok account is signed in in.
func newGrokHome() (string, error) {
	home := filepath.Join(grokAccountsDir(), strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(randomToken(9))))
	return home, os.MkdirAll(home, 0o700)
}

// addGrokLogin keeps the account just signed in in home, in use beside
// the others. Signed in again, an account keeps the home it had.
func addGrokLogin(home string) (string, error) {
	c, ok := readGrokCredential(home)
	if !ok || c.Email == "" {
		removeGrokHome(home)
		return "", errors.New("grok login finished without an account")
	}
	loginsMu.Lock()
	defer loginsMu.Unlock()
	ls := readLogins()
	for i := range ls {
		if ls[i].Agent == "grok" && strings.EqualFold(ls[i].User, c.Email) {
			if ls[i].Home != "" {
				// the fresh sign-in replaces the old one
				removeGrokHome(ls[i].Home)
				ls[i].Home = home
				ls[i].Seen = time.Now().UTC().Truncate(time.Second)
				return c.Email, writeLogins(ls)
			}
			removeGrokHome(home) // the CLI's own already
			return c.Email, nil
		}
	}
	ls = append(ls, savedLogin{Agent: "grok", User: c.Email, Home: home, On: true, Seen: time.Now().UTC().Truncate(time.Second)})
	return c.Email, writeLogins(ls)
}

// grokAlsoOn is the Grok accounts in use behind the first.
func grokAlsoOn(p Provider) []Provider {
	var out []Provider
	for _, g := range grokLogins() {
		if g.Active || !g.On {
			continue
		}
		a := *p.Account
		a.User, a.Plan, a.Home = g.User, g.Plan, g.Home
		q := p
		q.Account = &a
		out = append(out, q)
	}
	return out
}

var grokHomeUsage struct {
	sync.Mutex
	m map[string]loginUsageEntry // home
}

// grokLoginUsage is each Grok account's allowance, by user, as LoginUsage
// answers it.
func grokLoginUsage(ctx context.Context) map[string]SubscriptionQuota {
	out := map[string]SubscriptionQuota{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	u := &grokHomeUsage
	for _, g := range grokLogins() {
		u.Lock()
		e, ok := u.m[g.Home]
		u.Unlock()
		if ok && time.Since(e.at) < time.Minute {
			out[g.User] = e.q
			continue
		}
		wg.Add(1)
		go func(g grokLogin) {
			defer wg.Done()
			q := grokUsageAt(ctx, g.Home)
			if q.Error != "" && ok {
				q = e.q // a hiccup keeps what was known
			}
			u.Lock()
			if u.m == nil {
				u.m = map[string]loginUsageEntry{}
			}
			u.m[g.Home] = loginUsageEntry{time.Now(), q}
			u.Unlock()
			mu.Lock()
			out[g.User] = q
			mu.Unlock()
		}(g)
	}
	wg.Wait()
	return out
}

// grokUsageAt is the allowance of the account signed in in home.
func grokUsageAt(ctx context.Context, home string) SubscriptionQuota {
	q := SubscriptionQuota{Provider: "grok", Name: "Grok", Icon: "xai", Windows: []QuotaWindow{}}
	c, err := grokAccessToken(home, GrokExecutable(), false)
	if err == nil {
		q.Windows, err = grokWindows(ctx, c.Key)
	}
	if err != nil {
		q.Error = err.Error()
	}
	return q
}
