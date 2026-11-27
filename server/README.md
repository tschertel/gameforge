# Update check server

The app's check for new versions (off unless the user turns it on) asks this [Cloudflare Worker](https://developers.cloudflare.com/workers/) instead of GitHub. The Worker answers with the newest release, taken from GitHub, and counts the checks: per day, app version and system. That count is how the project knows roughly how many people use the app.

What the app sends: `GET /latest?version=2.22.0&os=windows&arch=amd64`, with the User-Agent `GameForge/2.22.0`.
What the Worker keeps: one number per day, version, system and processor (`2026-11-26, 2.22.0, windows, amd64, 41`), and the newest release. No IP address, no ID, no time of day, no request log.

If the Worker does not answer, the app asks GitHub directly, as before.

## Setting it up

In the Cloudflare dashboard:

1. **Storage & Databases → D1 → Create**: a database named `gameforge`. In its **Console**, run `schema.sql`.
2. **Compute (Workers) → Create → Start with Hello World**: a Worker named `gameforge`. **Edit code**, replace the code with `worker.js`, **Deploy**.
3. In the Worker: **Settings → Bindings → Add → D1 database**: variable name `DB`, database `gameforge`. Deploy again if asked.
4. In the Worker's **Settings → Observability**, leave **Workers Logs** off, so that no requests are logged.
5. Open `https://gameforge.<your-subdomain>.workers.dev/latest`: it shows the newest release. That address goes into `CountURL` in `internal/update/update.go`; the app uses `https://gameforge.tschertel.workers.dev/latest`.

The free plan is enough: 100,000 requests and 100,000 database writes a day.

## Reading the counts

The page `https://gameforge.<your-subdomain>.workers.dev/stats?key=…` shows the totals (today, 7 days, 30 days, all), the checks per week and system, and the versions in use. The key is the Worker's secret `STATS_KEY` (**Settings → Variables and Secrets → Add → Secret**): any long random text, such as the output of `openssl rand -hex 24`. Without the secret, or with a wrong key, the page answers "Not found", like any unknown address.

The same counts, in the D1 **Console**:

```sql
-- Checks per week and system, the last eight weeks.
SELECT strftime('%Y-%W', day) AS week, os, SUM(count) AS checks
FROM checks WHERE day >= date('now', '-56 days')
GROUP BY week, os ORDER BY week, os;

-- Which versions are in use, the last 30 days.
SELECT version, SUM(count) AS checks
FROM checks WHERE day >= date('now', '-30 days')
GROUP BY version ORDER BY checks DESC;
```

A check is counted each time an app looks for a new version: daily, weekly or monthly, as each user chose, and once more when someone clicks **Check now** in the About window. So the counts tell trends and shares, not exact numbers of people. Anyone can call the address, so the numbers can also be inflated on purpose; they are a guide, not a measurement. Only versions from 2.23.0 (the first that asks the Worker) up to the latest release are counted, so made-up versions add no rows.
