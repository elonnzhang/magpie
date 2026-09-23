// usemagpie.ai. Releases live on GitHub (yetone/magpie-releases); this
// worker turns the newest one into the update feed the app reads and into
// download links that never go stale.
//
//   /api/latest            {version, notes, url, published, assets: {name: {url, size, sha256}}}
//   /download              the Apple Silicon dmg
//   /download/mac-arm64    the same;  /download/mac-intel  the Intel dmg
//   /download/<file>       any file of the newest release, by name
//
// Everything else is the static site in public/.

const REPO = "yetone/magpie-releases";
const TTL = 300; // seconds the newest release is remembered

const SHORT = {
  "mac-arm64": "magpie-darwin-arm64.dmg",
  "mac-intel": "magpie-darwin-amd64.dmg",
  "mac-amd64": "magpie-darwin-amd64.dmg",
};

export default {
  async fetch(req, env, ctx) {
    const url = new URL(req.url);
    if (url.hostname.startsWith("www.")) {
      url.hostname = url.hostname.slice(4);
      return Response.redirect(url.toString(), 301);
    }
    if (url.pathname === "/api/latest") {
      const rel = await latest(ctx);
      if (!rel) return json({ error: "no release yet" }, 503);
      return json(rel, 200, { "Cache-Control": `public, max-age=${TTL}` });
    }
    if (url.pathname === "/download" || url.pathname.startsWith("/download/")) {
      const want = url.pathname.split("/")[2] || "mac-arm64";
      const rel = await latest(ctx);
      const asset = rel && rel.assets[SHORT[want] || want];
      if (!asset) return new Response("not found\n", { status: 404 });
      return Response.redirect(asset.url, 302);
    }
    return env.ASSETS.fetch(req);
  },
};

// latest is the newest release, condensed, with each file's SHA-256 taken
// from the release's SHA256SUMS.
async function latest(ctx) {
  const cache = caches.default;
  const key = new Request("https://usemagpie.ai/__latest");
  const hit = await cache.match(key);
  if (hit) return hit.json();

  const res = await fetch(`https://api.github.com/repos/${REPO}/releases/latest`, {
    headers: { "User-Agent": "usemagpie.ai", Accept: "application/vnd.github+json" },
  });
  if (!res.ok) return null;
  const gh = await res.json();
  const sums = {};
  const sumsAsset = gh.assets.find((a) => a.name === "SHA256SUMS");
  if (sumsAsset) {
    const r = await fetch(sumsAsset.browser_download_url, { headers: { "User-Agent": "usemagpie.ai" } });
    if (r.ok) {
      for (const line of (await r.text()).split("\n")) {
        const [hash, name] = line.trim().split(/\s+\*?/);
        if (hash && name) sums[name] = hash;
      }
    }
  }
  const rel = {
    version: gh.tag_name.replace(/^v/, ""),
    notes: gh.body || "",
    url: gh.html_url,
    published: gh.published_at,
    assets: {},
  };
  for (const a of gh.assets) {
    if (a.name === "SHA256SUMS") continue;
    rel.assets[a.name] = { url: a.browser_download_url, size: a.size, sha256: sums[a.name] || "" };
  }
  ctx.waitUntil(cache.put(key, json(rel, 200, { "Cache-Control": `max-age=${TTL}` })));
  return rel;
}

function json(v, status = 200, headers = {}) {
  return new Response(JSON.stringify(v), {
    status,
    headers: { "Content-Type": "application/json", "Access-Control-Allow-Origin": "*", ...headers },
  });
}
