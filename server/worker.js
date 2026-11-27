// GameForge's check for new versions, as a Cloudflare Worker.
//
// The app asks GET /latest?version=2.22.0&os=windows&arch=amd64 (only when
// the user turned the check on). The Worker answers with the newest release
// from GitHub and adds one to the count of checks of that day, version and
// system. Nothing else is kept: no IP address, no ID, no request log.

const GITHUB = "https://api.github.com/repos/tschertel/gameforge/releases/latest";
const SYSTEMS = new Set(["windows", "linux", "darwin"]);
const ARCHES = new Set(["amd64", "arm64"]);
const FRESH = 60 * 60 * 1000; // GitHub is asked at most once an hour
const FIRST = "2.23.0"; // the first version that asks this Worker

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
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

// latest is the newest release in the form GitHub gives it, kept in the
// database for an hour, so that GitHub's limits are never reached. If
// GitHub does not answer, the release kept last is used.
async function latest(env) {
  const kept = await env.DB.prepare("SELECT tag, page, fetched FROM release WHERE id = 1").first();
  if (kept && Date.now() - kept.fetched < FRESH) {
    return { tag_name: kept.tag, html_url: kept.page };
  }
  try {
    const resp = await fetch(GITHUB, {
      headers: { "User-Agent": "gameforge-update-worker", "Accept": "application/vnd.github+json" },
    });
    if (resp.ok) {
      const r = await resp.json();
      if (typeof r.tag_name === "string" && typeof r.html_url === "string") {
        await env.DB.prepare(
          "INSERT INTO release (id, tag, page, fetched) VALUES (1, ?1, ?2, ?3) " +
          "ON CONFLICT (id) DO UPDATE SET tag = ?1, page = ?2, fetched = ?3",
        ).bind(r.tag_name, r.html_url, Date.now()).run();
        return { tag_name: r.tag_name, html_url: r.html_url };
      }
    }
  } catch {
    // Use the release kept last, below.
  }
  return kept ? { tag_name: kept.tag, html_url: kept.page } : null;
}
