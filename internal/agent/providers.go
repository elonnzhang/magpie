package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yetone/dial/internal/catalog"
)

// Provider is a model vendor an agent can be pointed at. Each agent speaks
// one wire protocol, so a provider is offered to an agent only when it has
// a base URL for that protocol.
//
// dial ships a list of vendors whose endpoints speak the protocols natively;
// the user can add their own, or edit and hide the built-in ones. Everything
// but the built-in defaults lives in ~/.config/dial/providers.json.
type Provider struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	EnvKey    string   `json:"envKey"`              // environment variable that carries the API key
	Catalog   string   `json:"catalog,omitempty"`   // models.dev provider id for the model list
	Anthropic string   `json:"anthropic,omitempty"` // base URL of an Anthropic Messages API (Claude Code)
	Responses string   `json:"responses,omitempty"` // base URL of an OpenAI Responses API (Codex)
	Small     string   `json:"small,omitempty"`     // model for the small/fast slot, when the vendor has one
	Extra     []string `json:"models,omitempty"`    // the user's own model ids, on top of the catalog
	Website   string   `json:"website,omitempty"`   // the vendor's console
	KeysURL   string   `json:"keysUrl,omitempty"`   // where to create an API key
	Icon      string   `json:"icon,omitempty"`      // bundled icon name; empty means "derive one"
	Builtin   bool     `json:"builtin"`             // shipped with dial (may still be edited)
}

// builtins are the vendors dial knows out of the box. Only endpoints that
// speak the protocol natively are listed; nothing here needs a proxy.
var builtins = []Provider{
	{ID: "deepseek", Name: "DeepSeek", EnvKey: "DEEPSEEK_API_KEY", Catalog: "deepseek", Icon: "deepseek-color",
		Anthropic: "https://api.deepseek.com/anthropic", Responses: "https://api.deepseek.com/v1", Small: "deepseek-flash",
		Website: "https://platform.deepseek.com", KeysURL: "https://platform.deepseek.com/api_keys"},
	{ID: "kimi", Name: "Kimi", EnvKey: "MOONSHOT_API_KEY", Catalog: "moonshotai", Icon: "kimi",
		Anthropic: "https://api.moonshot.ai/anthropic",
		Website:   "https://platform.kimi.ai", KeysURL: "https://platform.kimi.ai/console/api-keys"},
	{ID: "kimi-cn", Name: "Kimi (China)", EnvKey: "MOONSHOT_API_KEY", Catalog: "moonshotai", Icon: "kimi",
		Anthropic: "https://api.moonshot.cn/anthropic",
		Website:   "https://platform.kimi.com", KeysURL: "https://platform.kimi.com/console/api-keys"},
	{ID: "glm", Name: "Zhipu GLM", EnvKey: "ZHIPU_API_KEY", Catalog: "zhipuai", Icon: "zhipu-color",
		Anthropic: "https://open.bigmodel.cn/api/anthropic",
		Website:   "https://open.bigmodel.cn", KeysURL: "https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys"},
	{ID: "zai", Name: "Z.ai GLM", EnvKey: "ZAI_API_KEY", Catalog: "zhipuai", Icon: "zai",
		Anthropic: "https://api.z.ai/api/anthropic",
		Website:   "https://z.ai", KeysURL: "https://z.ai/manage-apikey/apikey-list"},
	{ID: "minimax", Name: "MiniMax", EnvKey: "MINIMAX_API_KEY", Catalog: "minimax", Icon: "minimax-color",
		Anthropic: "https://api.minimax.io/anthropic",
		Website:   "https://platform.minimax.io", KeysURL: "https://platform.minimax.io/user-center/basic-information/interface-key"},
	{ID: "minimax-cn", Name: "MiniMax (China)", EnvKey: "MINIMAX_API_KEY", Catalog: "minimax", Icon: "minimax-color",
		Anthropic: "https://api.minimax.cn/anthropic",
		Website:   "https://platform.minimax.cn", KeysURL: "https://platform.minimax.cn/user-center/basic-information/interface-key"},
	{ID: "qwen", Name: "Qwen", EnvKey: "DASHSCOPE_API_KEY", Catalog: "alibaba", Icon: "qwen-color",
		Anthropic: "https://dashscope-intl.aliyuncs.com/apps/anthropic",
		Website:   "https://modelstudio.console.alibabacloud.com", KeysURL: "https://modelstudio.console.alibabacloud.com/?tab=playground#/api-key"},
	{ID: "qwen-cn", Name: "Qwen (China)", EnvKey: "DASHSCOPE_API_KEY", Catalog: "alibaba", Icon: "qwen-color",
		Anthropic: "https://dashscope.aliyuncs.com/apps/anthropic",
		Website:   "https://bailian.console.aliyun.com", KeysURL: "https://bailian.console.aliyun.com/?tab=model#/api-key"},
	{ID: "openrouter", Name: "OpenRouter", EnvKey: "OPENROUTER_API_KEY", Catalog: "openrouter", Icon: "openrouter",
		Anthropic: "https://openrouter.ai/api",
		Website:   "https://openrouter.ai", KeysURL: "https://openrouter.ai/keys"},
}

// ---- registry ---------------------------------------------------------------

type registry struct {
	Providers []Provider `json:"providers"` // user-defined, and edited copies of built-ins
	Hidden    []string   `json:"hidden,omitempty"`
}

// ProvidersPath is the file that holds the user's providers.
func ProvidersPath() string { return filepath.Join(filepath.Dir(KeysPath()), "providers.json") }

func loadRegistry() registry {
	var r registry
	if b, err := os.ReadFile(ProvidersPath()); err == nil {
		json.Unmarshal(b, &r)
	}
	return r
}

func saveRegistry(r registry) error {
	p := ProvidersPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o644)
}

// Providers is every provider in display order: the built-ins (with the
// user's edits, minus the hidden ones), then the user's own.
func Providers() []Provider {
	r := loadRegistry()
	edited := map[string]Provider{}
	for _, p := range r.Providers {
		edited[p.ID] = p
	}
	var out []Provider
	for _, b := range builtins {
		if contains(r.Hidden, b.ID) {
			continue
		}
		if e, ok := edited[b.ID]; ok {
			e.Builtin = true
			if e.Icon == "" {
				e.Icon = b.Icon
			}
			out = append(out, e)
			continue
		}
		b.Builtin = true
		out = append(out, b)
	}
	for _, p := range r.Providers {
		if builtin(p.ID) == nil {
			p.Builtin = false
			out = append(out, p)
		}
	}
	return out
}

func builtin(id string) *Provider {
	for i := range builtins {
		if builtins[i].ID == id {
			return &builtins[i]
		}
	}
	return nil
}

func provider(id string) *Provider {
	for _, p := range Providers() {
		if p.ID == id {
			return &p
		}
	}
	return nil
}

// FindProvider looks a provider up by id.
func FindProvider(id string) (*Provider, error) {
	if p := provider(strings.ToLower(strings.TrimSpace(id))); p != nil {
		return p, nil
	}
	return nil, fmt.Errorf("unknown provider %q", id)
}

var idRe = regexp.MustCompile(`[^a-z0-9]+`)

// ProviderID derives an id from a display name: "My Proxy" → "my-proxy".
func ProviderID(name string) string {
	return strings.Trim(idRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// SaveProvider adds or updates a provider. A built-in gets an edited copy;
// the defaults stay available through ResetProvider.
func SaveProvider(p Provider) error {
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		p.ID = ProviderID(p.Name)
	}
	if p.ID == "" || p.ID != ProviderID(p.ID) {
		return fmt.Errorf("provider id must be lowercase letters, digits and dashes, not %q", p.ID)
	}
	if p.Name == "" {
		p.Name = p.ID
	}
	p.EnvKey = strings.TrimSpace(p.EnvKey)
	if p.EnvKey == "" {
		p.EnvKey = strings.ToUpper(strings.ReplaceAll(p.ID, "-", "_")) + "_API_KEY"
	}
	if strings.ContainsAny(p.EnvKey, "= \n") {
		return fmt.Errorf("bad variable name %q", p.EnvKey)
	}
	for _, u := range []*string{&p.Anthropic, &p.Responses, &p.Website, &p.KeysURL} {
		*u = strings.TrimRight(strings.TrimSpace(*u), "/")
		if *u != "" && !strings.HasPrefix(*u, "http://") && !strings.HasPrefix(*u, "https://") {
			*u = "https://" + *u
		}
	}
	if p.Anthropic == "" && p.Responses == "" {
		return errors.New("a provider needs at least one endpoint: an Anthropic Messages URL (for Claude Code) or a Responses URL (for Codex)")
	}
	p.Extra = cleanModels(p.Extra)
	p.Builtin = false
	r := loadRegistry()
	r.Hidden = without(r.Hidden, p.ID)
	replaced := false
	for i := range r.Providers {
		if r.Providers[i].ID == p.ID {
			r.Providers[i] = p
			replaced = true
		}
	}
	if !replaced {
		if b := builtin(p.ID); b != nil && sameProvider(*b, p) {
			return saveRegistry(r) // edited back to the defaults: nothing to keep
		}
		r.Providers = append(r.Providers, p)
	}
	return saveRegistry(r)
}

// DeleteProvider removes a user provider, or hides a built-in one.
func DeleteProvider(id string) error {
	r := loadRegistry()
	var keep []Provider
	found := false
	for _, p := range r.Providers {
		if p.ID == id {
			found = true
			continue
		}
		keep = append(keep, p)
	}
	r.Providers = keep
	if builtin(id) != nil {
		found = true
		if !contains(r.Hidden, id) {
			r.Hidden = append(r.Hidden, id)
		}
	}
	if !found {
		return fmt.Errorf("unknown provider %q", id)
	}
	return saveRegistry(r)
}

// ResetProvider drops the user's edits to a built-in (and un-hides it).
func ResetProvider(id string) error {
	if builtin(id) == nil {
		return fmt.Errorf("%q is not a built-in provider", id)
	}
	r := loadRegistry()
	var keep []Provider
	for _, p := range r.Providers {
		if p.ID != id {
			keep = append(keep, p)
		}
	}
	r.Providers = keep
	r.Hidden = without(r.Hidden, id)
	return saveRegistry(r)
}

// HiddenProviders lists built-ins the user removed, so they can come back.
func HiddenProviders() []Provider {
	var out []Provider
	for _, id := range loadRegistry().Hidden {
		if b := builtin(id); b != nil {
			b.Builtin = true
			out = append(out, *b)
		}
	}
	return out
}

func sameProvider(a, b Provider) bool {
	a.Builtin, b.Builtin = false, false
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func cleanModels(ms []string) []string {
	var out []string
	for _, m := range ms {
		if m = strings.TrimSpace(m); m != "" && !contains(out, m) {
			out = append(out, m)
		}
	}
	return out
}

func without(xs []string, x string) []string {
	var out []string
	for _, v := range xs {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}

// providerByURL finds the provider whose base URL for the given protocol
// matches u (ignoring scheme and trailing slashes).
func providerByURL(u string, url func(Provider) string) *Provider {
	n := normURL(u)
	if n == "" {
		return nil
	}
	for _, p := range Providers() {
		if normURL(url(p)) == n {
			return &p
		}
	}
	return nil
}

func normURL(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(u), "https://"), "http://")
	return strings.ToLower(strings.TrimRight(u, "/"))
}

func hostOf(u string) string {
	h := normURL(u)
	if i := strings.Index(h, "/"); i > 0 {
		h = h[:i]
	}
	return h
}

// Host is the provider's API host, for display.
func (p Provider) Host() string {
	if p.Anthropic != "" {
		return hostOf(p.Anthropic)
	}
	return hostOf(p.Responses)
}

// Models lists the provider's models: the user's own first, then what the
// vendor itself reported (see RefreshModels), else the catalog's, newest
// first, minus experiments.
func (p Provider) Models() []catalog.Model {
	var out []catalog.Model
	for _, id := range p.Extra {
		out = append(out, catalog.Model{ID: id, Name: id})
	}
	known := catalog.Provider(p.Catalog)
	if live, _, ok := catalog.Live(p.ID); ok {
		for _, m := range catalog.Decorate(live, known) {
			if !contains(p.Extra, m.ID) {
				out = append(out, m)
			}
		}
		return out
	}
	for _, m := range known {
		if !contains(p.Extra, m.ID) && !strings.Contains(m.ID, "-exp") && !strings.Contains(m.ID, "preview") {
			out = append(out, m)
		}
	}
	return out
}

// LiveModels reports when the vendor's own list was last fetched.
func (p Provider) LiveModels() (n int, fetched time.Time, ok bool) {
	ms, t, ok := catalog.Live(p.ID)
	return len(ms), t, ok
}

// RefreshModels asks the vendor which models it serves and remembers the
// answer, so pickers stop offering names the vendor retired.
func (p Provider) RefreshModels(ctx context.Context) ([]catalog.Model, error) {
	key := Key(p.EnvKey)
	if key == "" {
		return nil, needKey(p)
	}
	var lastErr error
	for _, base := range []string{p.Responses, p.Anthropic} {
		if base == "" {
			continue
		}
		ms, err := catalog.Fetch(ctx, base, key)
		if err == nil {
			return ms, catalog.SaveLive(p.ID, base, ms)
		}
		lastErr = err
	}
	return nil, lastErr
}

// refreshQuietly is RefreshModels for the switch path: quick, and a failure
// just leaves the catalog in charge.
func (p Provider) refreshQuietly() {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	p.RefreshModels(ctx)
}

// option renders the provider as a picker entry for the given base URL.
func (p Provider) option(base string) Option {
	o := Option{Value: p.ID, Label: p.Name, Key: p.EnvKey, Icon: p.Icon, Note: hostOf(base) + " · $" + p.EnvKey}
	if Key(p.EnvKey) == "" {
		o.Note += " not set"
		o.NeedKey = true
	}
	return o
}

// sortReady puts providers whose key is present first, keeping order otherwise.
func sortReady(opts []Option) []Option {
	sort.SliceStable(opts, func(i, j int) bool { return !opts[i].NeedKey && opts[j].NeedKey })
	return opts
}

// ---- connectivity -----------------------------------------------------------

// TestResult is what a probe of one endpoint came back with.
type TestResult struct {
	Protocol string `json:"protocol"` // "anthropic" or "responses"
	OK       bool   `json:"ok"`
	Status   int    `json:"status,omitempty"`
	Millis   int64  `json:"ms"`
	Model    string `json:"model,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Test sends the smallest possible request to each of the provider's
// endpoints with the key dial has, and reports what came back.
func (p Provider) Test(ctx context.Context) []TestResult {
	key := Key(p.EnvKey)
	if key != "" {
		p.RefreshModels(ctx)
	}
	model := ""
	if ms := p.Models(); len(ms) > 0 {
		model = ms[0].ID
	}
	var out []TestResult
	if p.Anthropic != "" {
		body := fmt.Sprintf(`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`, model)
		out = append(out, probe(ctx, "anthropic", p.Anthropic+"/v1/messages", body, model, map[string]string{
			"x-api-key": key, "Authorization": "Bearer " + key, "anthropic-version": "2023-06-01",
		}))
	}
	if p.Responses != "" {
		body := fmt.Sprintf(`{"model":%q,"input":"hi","max_output_tokens":16}`, model)
		out = append(out, probe(ctx, "responses", p.Responses+"/responses", body, model, map[string]string{
			"Authorization": "Bearer " + key,
		}))
	}
	return out
}

func probe(ctx context.Context, proto, url, body, model string, headers map[string]string) TestResult {
	r := TestResult{Protocol: proto, Model: model}
	if model == "" {
		r.Error = "no model to try: add one to the provider, or sync the catalog"
		return r
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		r.Error = err.Error()
		return r
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	res, err := http.DefaultClient.Do(req)
	r.Millis = time.Since(start).Milliseconds()
	if err != nil {
		r.Error = strings.TrimPrefix(err.Error(), "Post \""+url+"\": ")
		return r
	}
	defer res.Body.Close()
	r.Status = res.StatusCode
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		r.OK = true
		return r
	}
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	r.Error = apiError(b, res.Status)
	return r
}

// apiError pulls the human message out of an error body when there is one.
func apiError(b []byte, fallback string) string {
	var v struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &v) == nil {
		if v.Error.Message != "" {
			return v.Error.Message
		}
		if v.Message != "" {
			return v.Message
		}
	}
	if s := strings.TrimSpace(string(b)); s != "" && len(s) < 200 && !strings.HasPrefix(s, "<") {
		return fallback + ": " + s
	}
	return fallback
}

// ---- API keys --------------------------------------------------------------
//
// Keys come from the environment first. For apps that do not inherit a
// shell (a desktop app, a GUI launcher) dial can hold a key itself, in a
// 0600 file next to its profiles, and writes it into the agent's config on
// switch.

// KeysPath is the file dial keeps API keys in.
func KeysPath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "dial", "keys")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "dial", "keys")
}

// Key returns the API key for an env var: the environment, else dial's store.
func Key(env string) string {
	if env == "" {
		return ""
	}
	if v := os.Getenv(env); v != "" {
		return v
	}
	return StoredKeys()[env]
}

// KeyState says where a key comes from: "env", "stored" or "" (nowhere).
func KeyState(env string) (state, masked string) {
	if env == "" {
		return "", ""
	}
	if v := os.Getenv(env); v != "" {
		return "env", Mask(v)
	}
	if v := StoredKeys()[env]; v != "" {
		return "stored", Mask(v)
	}
	return "", ""
}

// StoredKeys reads dial's key store.
func StoredKeys() map[string]string {
	out := map[string]string{}
	f, err := os.Open(KeysPath())
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok && k != "" {
			out[k] = v
		}
	}
	return out
}

// SetKey stores (or, with an empty value, forgets) a key.
func SetKey(env, value string) error {
	env = strings.TrimSpace(env)
	if env == "" || strings.ContainsAny(env, "= \n") {
		return fmt.Errorf("bad variable name %q", env)
	}
	keys := StoredKeys()
	if value == "" {
		delete(keys, env)
	} else {
		keys[env] = strings.TrimSpace(value)
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, k := range names {
		b.WriteString(k + "=" + keys[k] + "\n")
	}
	p := KeysPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Chmod(p, 0o600)
}

// needKey is the error for a provider whose key is nowhere to be found.
func needKey(p Provider) error {
	return fmt.Errorf("%s needs $%s: export it, or store it with `dial key %s <value>`", p.Name, p.EnvKey, p.EnvKey)
}

// Mask hides all but the ends of a secret.
func Mask(s string) string {
	if len(s) <= 8 {
		return strings.Repeat("•", len(s))
	}
	return s[:4] + "…" + s[len(s)-4:]
}

// ---- stash -----------------------------------------------------------------
//
// Leaving a vendor's home provider replaces the model with one the new
// vendor knows. The old choice is kept here so the way back restores it.

func stashPath() string { return filepath.Join(filepath.Dir(KeysPath()), "stash.json") }

func stashLoad() map[string]string {
	out := map[string]string{}
	if b, err := os.ReadFile(stashPath()); err == nil {
		json.Unmarshal(b, &out)
	}
	return out
}

// stash remembers values under keys; empty values are dropped.
func stash(kv map[string]string) {
	m := stashLoad()
	for k, v := range kv {
		if v == "" {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	os.MkdirAll(filepath.Dir(stashPath()), 0o755)
	os.WriteFile(stashPath(), b, 0o600)
}

// unstash returns a remembered value and forgets it.
func unstash(key string) string {
	m := stashLoad()
	v := m[key]
	if v != "" {
		delete(m, key)
		b, _ := json.MarshalIndent(m, "", "  ")
		os.WriteFile(stashPath(), b, 0o600)
	}
	return v
}
