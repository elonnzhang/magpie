# dial

One small dial for every coding agent's model.

`dial` is a single screen that lists each AI coding agent on your machine and
the model it is set to. Click a value, pick a model. That is the whole app.

It lives in the menu bar: click the icon and a panel drops down; the same
screen also opens as a normal window (`dial`, or *Open dial* in the tray menu),
and there is a terminal version (`dial tui`) and a plain CLI.

```
  ◉ dial

  ▸ Claude Code   claude-fable-5-1[1m]                        ~/.claude/settings.json
    Codex         gpt-6-astra   effort medium
    Gemini CLI    gemini-3.1-pro
    OpenCode      anthropic/claude-sonnet-5   small anthropic/claude-haiku-4-5
    Pi            openrouter/z-ai/glm-5.2:batch
    Goose         anthropic/claude-sonnet-5
    Cursor        auto
    Copilot CLI   claude-fable-5

  ↑↓ agent  ·  ←→ field  ·  ↵ change  ·  s save profile  ·  p profiles  ·  q quit
```

- **One small binary.** About 11 MB with the desktop app (it uses the system
  webview through [Wails](https://wails.io), nothing bundled), 7 MB for the
  terminal-only build. macOS, Linux and Windows.
- **Edits config files surgically.** Only the one key you change is touched;
  comments, ordering and indentation in your `settings.json`, `config.toml`,
  `opencode.jsonc` or `config.yaml` survive intact. Writes are atomic.
- **Real model lists.** When you switch to a provider, dial asks that vendor
  which models it serves (`GET /models` with your key) and offers exactly
  those; the [models.dev](https://models.dev) catalog, Codex's own model cache
  and a built-in list fill in when a vendor has no list. Anything can also be
  typed in.
- **Providers.** DeepSeek, Kimi, GLM, MiniMax, Qwen, OpenRouter and any
  Anthropic- or Responses-compatible gateway you add, each with its key, a
  connection test and one-click switching of every agent that can use it.
- **Profiles.** Snapshot every agent's settings under a name and switch all of
  them back in one move.
- **Real logos, no framework.** Plain HTML over the system webview; brand
  icons from [lobehub/icons](https://github.com/lobehub/lobe-icons).

## Agents

| Agent        | File                              | Fields          |
| ------------ | --------------------------------- | --------------- |
| Claude Code  | `~/.claude/settings.json`         | provider, model |
| Codex        | `~/.codex/config.toml`            | provider, model, effort |
| Gemini CLI   | `~/.gemini/settings.json`, `~/.gemini/.env` | auth, model |
| OpenCode     | `~/.config/opencode/opencode.json(c)` | model, small |
| Pi           | `~/.pi/agent/settings.json`       | model           |
| Goose        | `~/.config/goose/config.yaml`     | model           |
| Cursor CLI   | `~/.cursor/cli-config.json`       | model           |
| Copilot CLI  | `~/.copilot/settings.json`        | model           |
| Crush        | `~/.config/crush/crush.json`      | large, small    |

Provider-scoped agents (OpenCode, Pi, Goose, Crush) take `provider/model`.
Only agents that are installed or configured are shown.

## Providers

The *Providers* page (or `dial providers`) lists every vendor dial knows:
its endpoints, whether a key is present, how many models it serves and
which agents use it. Built-ins: DeepSeek, Kimi, Zhipu/Z.ai GLM, MiniMax,
Qwen and OpenRouter, each with a global and a China endpoint where the vendor
has one. Any Anthropic-Messages or OpenAI-Responses compatible endpoint can
be added; built-ins can be edited, hidden or reset.

```sh
dial providers                       # who is configured, who has a key, who uses what
dial provider deepseek               # endpoints, key, the models it serves
dial provider add "My Gateway" anthropic=https://gw.example.com/anthropic env=GW_API_KEY catalog=anthropic
dial provider edit kimi models=kimi-k3-preview
dial provider test deepseek          # one tiny request per endpoint, with latency
dial provider models deepseek        # re-fetch the vendor's model list
dial provider rm kimi                # hide a built-in (reset brings it back)
dial key DEEPSEEK_API_KEY sk-…       # keys for apps that see no shell env
```

Keys come from your shell environment first, then from
`~/.config/dial/keys` (mode 0600). The desktop app asks for a key the first
time you pick a provider that has none.

**Claude Code** — `dial claude provider deepseek` sets `ANTHROPIC_BASE_URL`,
`ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_MODEL` and the small-model variable in the
`env` block of `settings.json`; `provider anthropic` removes them and
restores whatever was there. Claude Code re-reads its settings, so a running
session picks the change up.

**Codex** — `dial codex provider deepseek` adds a `[model_providers.deepseek]`
table to `config.toml`, points `model_catalog_json` at
`~/.codex/dial-models.json` (a catalog dial writes from the vendor's live
model list) and keeps `model`/`effort` valid. `provider openai` removes all
of that; your ChatGPT sign-in is never touched. Any `[model_providers.*]`
table you wrote yourself shows up as a provider too. Codex builds its model
list once at start-up, so the Codex app (and open `codex` sessions) must be
restarted to see the new list; dial says so after the switch.

**Gemini CLI** — `dial gemini auth api-key|google|vertex` sets
`security.auth.selectedType`; the API key goes to `~/.gemini/.env`.

Only DeepSeek has been verified end to end against a real account so far;
the other built-ins follow the vendors' documented endpoints.

## Install

```sh
go install github.com/yetone/dial@latest
```

or build locally:

```sh
make build            # ./dial with the desktop app (needs cgo + the platform webview)
make app              # macOS: dial.app, a menu bar app with no Dock icon
make cli              # terminal-only build, no cgo, cross-compiles anywhere
make release          # dist/: native app build + cli builds for every platform
```

Linux needs `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` for the app build;
Windows uses the WebView2 runtime that ships with the OS.

## Use

```sh
dial                          # open the app: a window plus the menu bar icon
dial tray                     # menu bar icon only (use this in your login items)
dial tui                      # the same dial, in the terminal
dial ls                       # list every agent and its current settings
dial claude opus              # set a model (agent names accept prefixes: cc, oc, gem …)
dial codex gpt-5.6-sol
dial codex effort high        # other fields
dial codex xhigh              # bare effort levels are recognised too
dial codex provider deepseek  # run Codex against DeepSeek (needs DEEPSEEK_API_KEY)
dial claude provider kimi     # Claude Code through Kimi's Anthropic-compatible endpoint
dial gemini auth api-key
dial opencode anthropic/claude-sonnet-5
dial oc small anthropic/claude-haiku-4-5

dial save work                # snapshot everything as a profile
dial use work                 # switch back
dial profiles
dial rm work

dial sync                     # refresh the models.dev catalog and every live model list
```

In the app, click any value to open a filtered list; type to search or to
enter something that is not listed; `esc` closes the panel. Profiles are the
chips at the bottom: click to apply, `×` to delete, *+ save current* to add.
The *Providers* tab of the window opens each provider inline: endpoints, key,
catalog, extra models, *Test connection* and a button per agent that can use
it.

Keys in the terminal dial:

| Key        | Action                                |
| ---------- | ------------------------------------- |
| `↑` `↓`    | choose agent                          |
| `←` `→`    | choose field (model, effort, small …) |
| `↵`        | open the picker                       |
| type       | filter; enter accepts custom values   |
| `s`        | save current setup as a profile       |
| `p`        | apply or delete (`ctrl+d`) a profile  |
| `S`        | sync the model catalog                |
| `q`        | quit                                  |

Agents read their config at startup, so a running session keeps its model
until you start a new one.

## Files

- `~/.config/dial/profiles.json` — saved profiles
- `~/.config/dial/providers.json` — your providers and edits to built-ins
- `~/.config/dial/keys` — stored API keys (0600)
- `~/.config/dial/stash.json` — values dial replaced, restored on switch-back
- `~/.cache/dial/models.json` — models.dev catalog (OpenCode's cache at
  `~/.cache/opencode/models.json` is used when present)
- `~/.cache/dial/models/<provider>.json` — model lists fetched from vendors

`XDG_CONFIG_HOME` and `XDG_CACHE_HOME` are respected.
