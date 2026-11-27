// GameForge's check for new versions, as a Cloudflare Worker.
//
// The app asks GET /latest?version=2.22.0&os=windows&arch=amd64 (only when
// the user turned the check on). The Worker answers with the newest release
// from GitHub and adds one to the count of checks of that day, version and
// system. Nothing else is kept: no IP address, no ID, no request log.
// GET /stats?key=… shows the counts to whoever has the key (the secret
// STATS_KEY).

const PAGE = "https://github.com/tschertel/gameforge/releases/latest";
const GITHUB = "https://api.github.com/repos/tschertel/gameforge/releases/latest";
const SYSTEMS = new Set(["windows", "linux", "darwin"]);
const ARCHES = new Set(["amd64", "arm64"]);
const FRESH = 60 * 60 * 1000; // GitHub is asked at most once an hour
const FIRST = "2.23.0"; // the first version that asks this Worker

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    if (request.method === "GET" && url.pathname === "/stats") {
      return stats(env, url.searchParams.get("key") ?? "");
    }
    if (request.method !== "GET" || url.pathname !== "/latest") {
      return new Response("Not found", { status: 404 });
    }
    const release = await latest(env);
    if (!release) {
      return new Response("GitHub did not answer", { status: 502 });
    }
    const version = url.searchParams.get("version") ?? "";
    const os = url.searchParams.get("os") ?? "";
    const arch = url.searchParams.get("arch") ?? "";
    // Only well-formed checks of a version that can ask (from FIRST to the
    // latest release) are counted, so made-up versions add no rows; the
    // answer is the same for all.
    if (/^\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(version) && !newer(FIRST, version) &&
        !newer(version, release.tag_name) && SYSTEMS.has(os) && ARCHES.has(arch)) {
      const day = new Date().toISOString().slice(0, 10);
      ctx.waitUntil(env.DB.prepare(
        "INSERT INTO checks (day, version, os, arch, count) VALUES (?1, ?2, ?3, ?4, 1) " +
        "ON CONFLICT (day, version, os, arch) DO UPDATE SET count = count + 1",
      ).bind(day, version, os, arch).run());
    }
    return Response.json(release, { headers: { "Cache-Control": "no-store" } });
  },
};

// newer tells whether version a ("2.23.4") is newer than tag b ("v2.23.3").
function newer(a, b) {
  const x = a.split(".").map(Number);
  const y = b.replace(/^v/, "").split(".").map(Number);
  for (let i = 0; i < 3; i++) {
    if ((x[i] || 0) !== (y[i] || 0)) return (x[i] || 0) > (y[i] || 0);
  }
  return false;
}

// latest is the newest release in the form GitHub's API gives it, kept in
// the database for an hour. If GitHub does not answer, the release kept
// last is used.
async function latest(env) {
  const kept = await env.DB.prepare("SELECT tag, page, fetched FROM release WHERE id = 1").first();
  if (kept && Date.now() - kept.fetched < FRESH) {
    return { tag_name: kept.tag, html_url: kept.page };
  }
  const r = (await fromPage()) ?? (await fromAPI());
  if (r) {
    await env.DB.prepare(
      "INSERT INTO release (id, tag, page, fetched) VALUES (1, ?1, ?2, ?3) " +
      "ON CONFLICT (id) DO UPDATE SET tag = ?1, page = ?2, fetched = ?3",
    ).bind(r.tag_name, r.html_url, Date.now()).run();
    return r;
  }
  return kept ? { tag_name: kept.tag, html_url: kept.page } : null;
}

// fromPage reads the newest release from where GitHub's web page of the
// latest release redirects to. The page is not held to the API's limit of
// 60 requests an hour per address, which the addresses Workers share with
// everyone else's soon reach.
async function fromPage() {
  try {
    const resp = await fetch(PAGE, { redirect: "manual", headers: { "User-Agent": "gameforge-update-worker" } });
    const to = resp.headers.get("Location") ?? "";
    const m = to.match(/^https:\/\/github\.com\/tschertel\/gameforge\/releases\/tag\/(v\d+\.\d+\.\d+)$/);
    return m ? { tag_name: m[1], html_url: to } : null;
  } catch {
    return null;
  }
}

// fromAPI asks GitHub's API, in case the web page changes.
async function fromAPI() {
  try {
    const resp = await fetch(GITHUB, {
      headers: { "User-Agent": "gameforge-update-worker", "Accept": "application/vnd.github+json" },
    });
    if (!resp.ok) return null;
    const r = await resp.json();
    if (typeof r.tag_name === "string" && typeof r.html_url === "string") {
      return { tag_name: r.tag_name, html_url: r.html_url };
    }
  } catch {
    // null below
  }
  return null;
}

// stats shows the counts, to whoever gives the key kept in the Worker's
// secret STATS_KEY; anyone else gets the same answer as an unknown address.
async function stats(env, key) {
  if (!env.STATS_KEY || !(await sameKey(key, env.STATS_KEY))) {
    return new Response("Not found", { status: 404 });
  }
  const sum = (where) => env.DB.prepare(`SELECT COALESCE(SUM(count), 0) AS n FROM checks ${where}`).first("n");
  const [today, week, month, all] = await Promise.all([
    sum("WHERE day = date('now')"),
    sum("WHERE day >= date('now', '-6 days')"),
    sum("WHERE day >= date('now', '-29 days')"),
    sum(""),
  ]);
  const weeks = (await env.DB.prepare(
    "SELECT strftime('%Y-%W', day) AS week, os, SUM(count) AS n FROM checks " +
    "WHERE day >= date('now', '-55 days') GROUP BY week, os ORDER BY week, os",
  ).all()).results;
  const versions = (await env.DB.prepare(
    "SELECT version, os, arch, SUM(count) AS n FROM checks " +
    "WHERE day >= date('now', '-29 days') GROUP BY version, os, arch",
  ).all()).results;
  versions.sort((a, b) => (newer(a.version, b.version) ? 1 : newer(b.version, a.version) ? -1 : b.n - a.n));

  const esc = (v) => String(v).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const row = (cells, tag = "td") => "<tr>" + cells.map((c) => `<${tag}>${esc(c)}</${tag}>`).join("") + "</tr>";
  const html = `<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><meta name="robots" content="noindex">
<title>GameForge checks</title><style>
:root { color-scheme: light dark; }
body { font: 15px/1.45 system-ui, sans-serif; margin: 2em auto; max-width: 48em; padding: 0 1em; }
table { border-collapse: collapse; margin: .5em 0 1.5em; }
th, td { text-align: left; padding: .3em .8em; border-bottom: 1px solid #8884; }
td:last-child, th:last-child { text-align: right; }
.n { font-size: 1.6em; font-weight: 600; }
</style></head><body>
<h1>GameForge: checks for new versions</h1>
<p>Each check is an app looking for a new version: daily, weekly or monthly, as each user chose. Days are UTC.</p>
<table>${row(["Today", "Last 7 days", "Last 30 days", "All"], "th")}<tr>${[today, week, month, all].map((n) => `<td class="n">${esc(n)}</td>`).join("")}</tr></table>
<h2>Per week and system, last 8 weeks</h2>
<table>${row(["Week", "System", "Checks"], "th")}${weeks.map((r) => row([r.week, r.os, r.n])).join("")}</table>
<h2>Versions, last 30 days</h2>
<table>${row(["Version", "System", "Processor", "Checks"], "th")}${versions.map((r) => row([r.version, r.os, r.arch, r.n])).join("")}</table>
</body></html>`;
  return new Response(html, {
    headers: { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store" },
  });
}

// sameKey compares the keys in constant time, so the time taken tells
// nothing about how much of a guess was right.
async function sameKey(a, b) {
  const enc = new TextEncoder();
  const [x, y] = await Promise.all([a, b].map((s) => crypto.subtle.digest("SHA-256", enc.encode(s))));
  return crypto.subtle.timingSafeEqual(x, y);
}
