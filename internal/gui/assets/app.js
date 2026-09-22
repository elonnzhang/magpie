// dial — one state object per view, rendered into a list. No framework.
const $ = (s) => document.querySelector(s);
const params = new URLSearchParams(location.search);
const mode = params.get("mode") || "window";
document.body.classList.add(mode);
if (params.get("theme")) document.documentElement.dataset.theme = params.get("theme");

let state = { agents: [], profiles: [], catalog: "" };
let providers = null; // { providers, hidden, catalogs }
let view = "agents";
let pick = null; // { agent, field, options, items, cursor, anchor, key?, keyFor?, onCommit? }
let editing = null; // provider id being edited, "" for a new one
let draft = null; // the editor's working copy

async function api(path, body) {
  const res = await fetch("/api/" + path, {
    method: body === undefined ? "GET" : "POST",
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204) return null;
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}
function svg(d, size = 12, stroke = 1.6) {
  const s = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  s.setAttribute("viewBox", "0 0 16 16");
  s.setAttribute("width", size);
  s.setAttribute("height", size);
  s.innerHTML = `<path d="${d}" fill="none" stroke="currentColor" stroke-width="${stroke}" stroke-linecap="round" stroke-linejoin="round"/>`;
  return s;
}
const CHEV = "m5.5 6.5 2.5 2.5 2.5-2.5";
const CHEV_R = "m6.5 4.5 3 3.5-3 3.5";
const CHECK = "m3.5 8.5 3 3 6-7";

// A brand icon: colour logos are images, mono logos take the text colour,
// anything unknown gets a two-letter monogram tinted by its name.
function icon(name, label) {
  const e = el("span", "ic");
  if (name) {
    if (name.endsWith("-color") || name === "crush") {
      const img = el("img");
      img.src = `icons/${name}.${name === "crush" ? "png" : "svg"}`;
      img.alt = "";
      img.draggable = false;
      e.append(img);
    } else {
      const m = el("span", "mask");
      m.style.setProperty("--i", `url(icons/${name}.svg)`);
      e.append(m);
    }
    return e;
  }
  const w = (label || "?").split(/\s+/);
  e.classList.add("monogram");
  e.textContent = (w.length > 1 ? w[0][0] + w[1][0] : (label || "?").slice(0, 2)).toUpperCase();
  let h = 0;
  for (const ch of label || "") h = (h * 31 + ch.charCodeAt(0)) % 360;
  e.style.setProperty("--h", h);
  return e;
}

function status(msg, kind = "", ms = 3500) {
  const s = $("#status");
  s.textContent = msg;
  s.title = msg;
  s.className = "status " + kind;
  clearTimeout(status.t);
  if (msg) status.t = setTimeout(() => { s.textContent = ""; s.className = "status"; }, ms);
}

// ---------- agents view ----------

function optionFor(field, value) {
  return field.options.find((o) => o.value === value);
}

function renderAgents() {
  const list = $("#agents");
  list.replaceChildren();
  if (!state.agents.length) {
    const e = el("div", "empty-state");
    e.append(el("b", "", "No coding agents found"), el("span", "", "Install Claude Code, Codex, Gemini CLI, OpenCode… and dial will list them here."));
    list.append(e);
  }
  for (const a of state.agents) {
    const row = el("div", "row agent");
    row.dataset.id = a.id;
    row.title = a.path;
    const who = el("div", "who");
    who.append(el("div", "name", a.name));
    const fields = el("div", "fields");
    for (const f of a.fields) {
      const b = el("button", "field");
      const opt = optionFor(f, f.value);
      b.title = `${f.label}: ${f.value || "agent default"}`;
      if (opt?.icon || (f.key === "provider" && f.value)) b.append(icon(opt?.icon, opt?.label || f.value));
      else if (f.label !== "model" || !f.value) b.append(el("span", "k", f.label));
      b.append(el("span", "v" + (f.value ? "" : " empty"), opt?.label || f.value || "default"));
      const c = el("span", "chev");
      c.append(svg(CHEV, 11, 1.7));
      b.append(c);
      b.onclick = (ev) => openPicker(a, f, b, ev);
      fields.append(b);
    }
    row.append(icon(a.icon, a.name), who, fields);
    list.append(row);
  }

  const chips = $("#profiles");
  chips.replaceChildren();
  if (!state.profiles.length) chips.append(el("span", "hint", "Save the current setup to switch everything back in one click."));
  for (const p of state.profiles) {
    const c = el("button", "chip");
    c.title = p.summary;
    c.append(el("span", "", p.name));
    const x = el("span", "x", "×");
    x.title = "Delete profile";
    x.onclick = (ev) => { ev.stopPropagation(); profileAction("delete", p.name); };
    c.append(x);
    c.onclick = () => profileAction("use", p.name);
    chips.append(c);
  }
  fit();
}

// The tray panel has no scrollbars to speak of, so it grows to fit instead.
function fit() {
  if (mode !== "panel") return;
  const h = $(".top").offsetHeight + $("#agents").offsetHeight + $(".profiles").offsetHeight + $(".foot").offsetHeight + 4;
  if (h !== fit.last) { fit.last = h; api("window/fit?h=" + h, {}); }
}

async function load() {
  try {
    state = await api("state");
    renderAgents();
    if (view === "providers") await loadProviders();
  } catch (e) {
    status(e.message, "err");
  }
}

// ---------- picker ----------

function score(q, o) {
  if (!q) return 1;
  const lv = o.value.toLowerCase(), ll = (o.label || "").toLowerCase();
  if (lv === q || ll === q) return 100;
  if (lv.startsWith(q) || ll.startsWith(q)) return 60;
  if (lv.includes(q) || ll.includes(q)) return 40;
  let i = 0;
  for (const ch of lv) if (ch === q[i]) i++;
  if (i === q.length) return 20;
  if (o.note && o.note.toLowerCase().includes(q)) return 10;
  return 0;
}

function placePop(anchor, w, h) {
  const pop = $("#pop");
  const r = anchor.getBoundingClientRect(), pad = 8;
  pop.style.width = w + "px";
  let x = Math.min(r.left, innerWidth - w - pad);
  let y = r.bottom + 5;
  pop.classList.remove("up");
  if (y + h > innerHeight - pad && r.top - 5 - h >= pad) { y = r.top - 5 - h; pop.classList.add("up"); }
  else if (y + h > innerHeight - pad) y = Math.max(pad, innerHeight - pad - h);
  pop.style.left = Math.max(pad, x) + "px";
  pop.style.top = y + "px";
}

function openPicker(agent, field, anchor, ev) {
  ev.stopPropagation();
  closePicker();
  const cur = field.value;
  // current value first, then the rest in catalog order
  const options = [...field.options];
  const i = options.findIndex((o) => o.value === cur);
  if (i > 0) options.unshift(...options.splice(i, 1));
  else if (i < 0 && cur) options.unshift({ value: cur, note: "current value" });
  pick = { agent, field, options, anchor, cursor: 0 };
  anchor.classList.add("open");
  const pop = $("#pop");
  pop.hidden = false;
  placePop(anchor, options.some((o) => o.note && o.note !== o.value) ? 360 : 300, 340);
  const q = $("#q");
  q.value = "";
  q.placeholder = field.label === "model" ? "Filter, or type any model id…" : `Filter ${field.label}…`;
  filter();
  q.focus();
}

function filter() {
  if (!pick) return;
  const q = $("#q").value.trim().toLowerCase();
  const scored = pick.options.map((o) => ({ o, s: score(q, o) })).filter((x) => x.s > 0).sort((a, b) => b.s - a.s);
  pick.items = scored.map((x) => x.o);
  const typed = $("#q").value.trim();
  if (typed && pick.field.label !== "provider" && pick.field.label !== "auth" && !pick.items.some((o) => o.value === typed)) {
    pick.items.push({ value: typed, note: "use as typed", custom: true });
  }
  pick.cursor = 0;
  renderList();
}

function renderList() {
  const list = $("#list");
  list.replaceChildren();
  if (!pick.items.length) { list.append(el("div", "none", "No matches.")); return; }
  const hasIcons = pick.items.some((o) => o.icon);
  pick.items.forEach((o, idx) => {
    const li = el("li", (idx === pick.cursor ? "sel" : "") + (o.value === pick.field.value ? " cur" : "") + (o.custom ? " custom" : ""));
    if (hasIcons) li.append(icon(o.icon, o.label || o.value));
    li.append(el("span", "v", o.label || o.value));
    const note = o.note && o.note !== (o.label || o.value) ? o.note : "";
    if (note) li.append(el("span", "n" + (o.needKey ? " nokey" : ""), note));
    const ck = el("span", "check");
    ck.append(svg(CHECK, 12, 1.8));
    li.append(ck);
    li.onmousemove = () => { if (pick.cursor !== idx) { pick.cursor = idx; renderList(); } };
    li.onclick = () => commit(o.value);
    list.append(li);
  });
  list.children[pick.cursor]?.scrollIntoView({ block: "nearest" });
}

function move(d) {
  if (!pick || !pick.items.length) return;
  pick.cursor = (pick.cursor + d + pick.items.length) % pick.items.length;
  renderList();
}

async function commit(value) {
  if (!pick || !value) return;
  const { agent, field } = pick;
  const opt = pick.options.find((o) => o.value === value);
  if (opt?.needKey && !pick.key) return askKey(opt);
  const key = pick.key;
  closePicker();
  if (value === field.value && !key) return;
  try {
    state = await api("set", { agent: agent.id, field: field.key, value, key });
    renderAgents();
    const b = document.querySelector(`.agent[data-id="${agent.id}"] .field:nth-child(${agent.fields.indexOf(field) + 1})`);
    b?.classList.add("flash");
    const shown = opt?.label || value;
    if (state.notice) status(`${agent.name} → ${shown}. ${state.notice}`, "warn", 9000);
    else status(`${agent.name} ${field.label} → ${shown}`, "ok");
    if (providers) loadProviders();
  } catch (e) {
    status(e.message, "err");
  }
}

// A provider whose key is nowhere yet: the popover turns into a key prompt.
// The key goes to dial's store, then the switch proceeds as normal.
function askKey(opt) {
  const pop = $("#pop");
  pop.classList.add("keying");
  const q = $("#q");
  q.type = "password";
  q.value = "";
  q.placeholder = `Paste your ${opt.key}, then ↩`;
  pick.items = [];
  pick.keyFor = opt;
  const list = $("#list");
  list.replaceChildren();
  const li = el("div", "none");
  const b = el("b", "", opt.label || opt.value);
  li.append(b, ` needs an API key. dial keeps it in ~/.config/dial/keys (mode 0600) and writes it into the agent's config when you switch.`);
  list.append(li);
  q.focus();
}

function closePicker() {
  if (!pick) return;
  pick.anchor.classList.remove("open");
  const pop = $("#pop");
  pop.hidden = true;
  pop.classList.remove("keying");
  $("#q").type = "text";
  pick = null;
}

$("#q").addEventListener("input", () => { if (!pick?.keyFor) filter(); });
$("#q").addEventListener("keydown", (e) => {
  if (pick?.keyFor) {
    if (e.key === "Enter") {
      e.preventDefault();
      const v = $("#q").value.trim();
      if (!v) return;
      pick.key = { env: pick.keyFor.key, value: v };
      commit(pick.keyFor.value);
    } else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); closePicker(); }
    return;
  }
  if (e.key === "ArrowDown" || (e.ctrlKey && e.key === "n")) { e.preventDefault(); move(1); }
  else if (e.key === "ArrowUp" || (e.ctrlKey && e.key === "p")) { e.preventDefault(); move(-1); }
  else if (e.key === "Enter") { e.preventDefault(); commit(pick?.items[pick.cursor]?.value); }
  else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); closePicker(); }
});
document.addEventListener("mousedown", (e) => { if (pick && !$("#pop").contains(e.target)) closePicker(); });
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && !pick && mode === "panel") api("window/hide", {});
});

// ---------- profiles ----------

async function profileAction(action, name) {
  try {
    const data = await api("profile/" + action, { name });
    state = data;
    renderAgents();
    if (action === "use") status(`${name} applied · ${data.changed} setting${data.changed === 1 ? "" : "s"} changed`, "ok");
    else if (action === "save") status(`Saved ${name}`, "ok");
    else status(`Deleted ${name}`);
  } catch (e) {
    status(e.message, "err");
  }
}

$("#save").onclick = () => {
  const chips = $("#profiles");
  if (chips.querySelector(".chip-input")) return;
  const input = el("input", "chip-input");
  input.placeholder = "Profile name";
  input.onkeydown = (e) => {
    if (e.key === "Enter" && input.value.trim()) profileAction("save", input.value.trim());
    else if (e.key === "Escape") input.remove();
    e.stopPropagation();
  };
  input.onblur = () => setTimeout(() => input.remove(), 100);
  chips.prepend(input);
  input.focus();
};

// ---------- providers view ----------

async function loadProviders() {
  providers = await api("providers");
  renderProviders();
}

function keyLabel(p) {
  if (p.key.state === "env") return `$${p.envKey}`;
  if (p.key.state === "stored") return "key saved";
  return "no key";
}

function renderProviders() {
  const list = $("#providers");
  list.replaceChildren();
  for (const p of providers.providers) {
    const row = el("div", "row provider" + (editing === p.id ? " selected" : ""));
    row.dataset.id = p.id;
    const who = el("div", "who");
    const name = el("div", "name", p.name);
    if (!p.builtin) name.append(el("span", "badge", "custom"));
    who.append(name, el("div", "sub", p.host + (p.models.count ? ` · ${p.models.count} models` : "")));
    const uses = el("div", "uses");
    for (const a of p.agents) {
      const b = el("button", "use" + (a.current ? " on" : ""));
      b.title = a.current ? `${a.name} uses ${p.name}` : `Switch ${a.name} to ${p.name}`;
      b.append(icon(a.icon, a.name));
      b.onclick = (ev) => { ev.stopPropagation(); switchAgent(a, p, b); };
      uses.append(b);
    }
    const key = el("span", "key " + (p.key.state ? "on" : "none"), keyLabel(p));
    key.title = p.key.state ? `${p.envKey} ${p.key.masked}` : `${p.envKey} is not set`;
    const chev = el("span", "chev");
    chev.append(svg(CHEV_R, 11, 1.7));
    row.append(icon(p.icon, p.name), who, uses, key, chev);
    row.onclick = () => { editing = editing === p.id ? null : p.id; draft = null; renderProviders(); };
    list.append(row);
    if (editing === p.id) list.append(renderEditor(p));
  }
  if (editing === "") list.append(renderEditor());
  list.querySelector(".editor")?.scrollIntoView({ block: "nearest" });
  const hl = $("#hiddenLine");
  hl.replaceChildren();
  if (providers.hidden.length) {
    hl.append("Hidden: ");
    providers.hidden.forEach((h, i) => {
      const b = el("button", "", h.name);
      b.title = `Show ${h.name} again`;
      b.onclick = () => providerAction("reset", { id: h.id }, `${h.name} is back`);
      if (i) hl.append(", ");
      hl.append(b);
    });
  }
}

// Switch an agent to a provider straight from the providers page. A missing
// key opens the editor with the key field ready.
async function switchAgent(a, p, btn) {
  if (a.current) return;
  if (!p.key.state) {
    editing = p.id; draft = null;
    renderProviders();
    status(`${p.name} needs $${p.envKey} first`, "warn");
    document.querySelector(".editor input[type=password]")?.focus();
    return;
  }
  btn.classList.add("busy");
  try {
    state = await api("set", { agent: a.id, field: "provider", value: p.id });
    renderAgents();
    await loadProviders();
    if (state.notice) status(`${a.name} → ${p.name}. ${state.notice}`, "warn", 9000);
    else status(`${a.name} → ${p.name}`, "ok");
  } catch (e) {
    btn.classList.remove("busy");
    status(e.message, "err");
  }
}

async function providerAction(action, body, okMsg) {
  try {
    providers = await api("provider/" + action, body);
    if (action !== "save" || editing === "") editing = null;
    draft = null;
    renderProviders();
    state = await api("state");
    renderAgents();
    if (okMsg) status(okMsg, "ok");
  } catch (e) {
    status(e.message, "err");
  }
}

function field(label, control, hint) {
  const l = el("label", "", label);
  const wrap = el("div");
  wrap.append(control);
  if (hint) wrap.append(el("div", "hint", hint));
  return [l, wrap];
}
function input(value, placeholder, type = "text") {
  const i = el("input");
  i.type = type;
  i.value = value || "";
  i.placeholder = placeholder || "";
  i.spellcheck = false;
  i.autocomplete = "off";
  i.onkeydown = (e) => { e.stopPropagation(); if (e.key === "Escape") { editing = null; draft = null; renderProviders(); } };
  return i;
}

function renderEditor(p) {
  const isNew = !p;
  draft = draft || (p ? { ...p, extra: [...p.extra] } : { id: "", name: "", envKey: "", anthropic: "", responses: "", catalog: "", website: "", keysUrl: "", builtin: false, extra: [] });
  const ed = el("div", "editor");
  ed.onclick = (e) => e.stopPropagation();

  const name = input(draft.name, "e.g. My Gateway");
  name.oninput = () => { draft.name = name.value; if (isNew) { draft.id = slug(name.value); id.value = draft.id; } };
  const id = input(draft.id, "auto");
  id.disabled = !isNew;
  id.oninput = () => { draft.id = id.value; };
  const idWrap = el("div", "pair");
  idWrap.append(name, id);
  id.style.flex = "0 0 130px";
  id.title = "Identifier used in agent configs";
  ed.append(...field("Name", idWrap, isNew ? "The id on the right is what agent configs will reference." : ""));

  const anth = input(draft.anthropic, "https://…/anthropic", "url");
  anth.oninput = () => { draft.anthropic = anth.value; };
  ed.append(...field("Anthropic API", anth, "Base URL of a Messages-compatible endpoint. Used by Claude Code."));
  const resp = input(draft.responses, "https://…/v1", "url");
  resp.oninput = () => { draft.responses = resp.value; };
  ed.append(...field("Responses API", resp, "Base URL of an OpenAI Responses-compatible endpoint. Used by Codex."));

  const env = input(draft.envKey, "MY_API_KEY");
  env.className = "env";
  env.oninput = () => { draft.envKey = env.value; };
  const key = input("", p?.key?.state === "env" ? `set in your shell · ${p.key.masked}` : p?.key?.state === "stored" ? `saved · ${p.key.masked}` : "paste an API key", "password");
  key.oninput = () => { draft.key = key.value; };
  const side = el("div", "side");
  const eye = el("button", "text", "Show");
  eye.onclick = () => { key.type = key.type === "password" ? "text" : "password"; eye.textContent = key.type === "password" ? "Show" : "Hide"; };
  side.append(eye);
  if (p?.key?.state === "stored") {
    const forget = el("button", "text danger", "Forget");
    forget.onclick = async () => {
      try { providers = await api("key", { env: p.envKey, value: "" }); status(`Forgot $${p.envKey}`); renderProviders(); } catch (e) { status(e.message, "err"); }
    };
    side.append(forget);
  }
  const keyWrap = el("div", "pair");
  keyWrap.append(key, env, side);
  ed.append(...field("API key", keyWrap, "Kept in ~/.config/dial/keys (0600). A variable already in your shell wins."));

  const cat = input(draft.catalog, "models.dev provider id, e.g. deepseek");
  cat.setAttribute("list", "catalogs");
  cat.oninput = () => { draft.catalog = cat.value; };
  const dl = el("datalist");
  dl.id = "catalogs";
  for (const c of providers.catalogs) dl.append(new Option(c));
  const catWrap = el("div");
  catWrap.append(cat, dl);
  ed.append(...field("Catalog", catWrap, "Model names and reasoning levels, used until the vendor's own list is fetched."));

  const extra = input(draft.extra.join(", "), "extra model ids, comma separated");
  extra.oninput = () => { draft.extra = extra.value.split(/[,\s]+/).filter(Boolean); };
  ed.append(...field("Own models", extra));

  if (p) {
    const m = el("div", "models");
    if (p.models.live) m.append(el("span", "", `${p.models.count} models from ${p.host}`), el("span", "muted", `· ${p.models.fetched}`));
    else m.append(el("span", "", `${p.models.count} models from the catalog`));
    const refresh = el("button", "text", "Refresh");
    refresh.title = "Ask the vendor which models it serves";
    refresh.onclick = async () => {
      refresh.classList.add("busy");
      try {
        const r = await api("provider/models", { id: p.id });
        status(`${p.name}: ${r.models.length} models`, "ok");
        await loadProviders();
      } catch (e) { status(e.message, "err"); refresh.classList.remove("busy"); }
    };
    m.append(refresh);
    ed.append(...field("Models", m));
  }

  const links = el("div", "links");
  const site = draft.website || (draft.anthropic || draft.responses ? "https://" + hostOf(draft.anthropic || draft.responses) : "");
  if (site) { const b = el("button", "", "Website ↗"); b.onclick = () => api("open", { url: site }); links.append(b); }
  if (draft.keysUrl) { const b = el("button", "", "Get an API key ↗"); b.onclick = () => api("open", { url: draft.keysUrl }); links.append(b); }
  if (links.children.length) ed.append(...field("", links));

  const bar = el("div", "bar");
  const results = el("div", "results");
  if (p) {
    const test = el("button", "text", "Test connection");
    test.onclick = async () => {
      test.classList.add("busy");
      results.replaceChildren(el("span", "res", "testing…"));
      try {
        const r = await api("provider/test", { id: p.id });
        results.replaceChildren();
        for (const t of r.results) {
          const b = el("span", "res " + (t.ok ? "ok" : "bad"));
          b.append(el("span", "", `${t.protocol} ${t.ok ? "✓" : "✗"}`));
          if (t.ok) b.append(el("span", "ms", `${t.ms} ms`));
          else b.append(el("span", "", t.status ? `${t.status} · ${t.error}` : t.error));
          b.title = t.model ? `model ${t.model}` : "";
          results.append(b);
        }
      } catch (e) { results.replaceChildren(); status(e.message, "err"); }
      test.classList.remove("busy");
    };
    bar.append(test, results);
  }
  bar.append(el("span", "grow"));
  if (p && p.builtin) {
    const reset = el("button", "text", "Reset");
    reset.title = "Back to dial's defaults";
    reset.onclick = () => providerAction("reset", { id: p.id }, `${p.name} reset`);
    const hide = el("button", "text danger", "Hide");
    hide.title = "Hide this built-in provider";
    hide.onclick = () => providerAction("delete", { id: p.id }, `${p.name} hidden`);
    bar.append(reset, hide);
  } else if (p) {
    const del = el("button", "text danger", "Delete");
    del.onclick = () => providerAction("delete", { id: p.id }, `${p.name} deleted`);
    bar.append(del);
  }
  const cancel = el("button", "text", "Cancel");
  cancel.onclick = () => { editing = null; draft = null; renderProviders(); };
  const save = el("button", "text primary", isNew ? "Add provider" : "Save");
  save.onclick = () => {
    const body = { id: draft.id, name: draft.name, envKey: draft.envKey, catalog: draft.catalog, anthropic: draft.anthropic, responses: draft.responses, small: draft.small, models: draft.extra, website: draft.website, keysUrl: draft.keysUrl, icon: draft.icon, key: draft.key || "" };
    providerAction("save", body, `${draft.name || draft.id} saved`);
  };
  bar.append(cancel, save);
  ed.append(bar);
  setTimeout(() => (isNew ? name : null)?.focus(), 0);
  return ed;
}

function slug(s) { return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, ""); }
function hostOf(u) { try { return new URL(u.includes("://") ? u : "https://" + u).host; } catch { return ""; } }

$("#addProvider").onclick = () => { editing = ""; draft = null; renderProviders(); };

// ---------- header / footer ----------

function show(v) {
  view = v;
  for (const b of $("#nav").children) b.classList.toggle("on", b.dataset.view === v);
  $("#view-agents").hidden = v !== "agents";
  $("#view-providers").hidden = v !== "providers";
  closePicker();
  if (v === "providers") loadProviders().catch((e) => status(e.message, "err"));
}
for (const b of $("#nav").children) b.onclick = () => { show(b.dataset.view); b.blur(); };

$("#sync").onclick = async () => {
  const b = $("#sync");
  b.classList.add("spin");
  try {
    state = await api("sync", {});
    renderAgents();
    if (providers) await loadProviders();
    status("Model catalog refreshed", "ok");
  } catch (e) {
    status("Sync failed: " + e.message, "err");
  } finally {
    b.classList.remove("spin");
  }
};
$("#open").onclick = () => api("window/main", {});
$("#openMain").onclick = () => api("window/main", {});
$("#quit").onclick = () => api("window/quit", {});
if (mode === "window") { $("#open").remove(); $("#openMain").remove(); $("#quit").remove(); }
else { $("#nav").remove(); }

// Config files may change underneath us (another dial, an editor); reload when
// the panel comes back into view.
document.addEventListener("visibilitychange", () => { if (!document.hidden) load(); });
window.addEventListener("focus", load);
load();
