# glancectl

A terminal dashboard that reads the same [Glance](https://github.com/glanceapp/glance) config you already have. Three panes: active alerts and [`just`](https://github.com/casey/just) recipes you can run, a feed of your custom-api widgets, and bookmarks you can launch.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## What it reuses from Glance

- `monitor` widgets → ignored. Service health comes from the `system*` widget's status grid instead, so glancectl does not re-probe what your monitoring already checks.
- `bookmarks` widgets → **Bookmarks** pane (`enter` opens in `$BROWSER`).
- `custom-api` widgets, by title:
  - one matching `alert*` → **Alerts** pane (top left) plus a footer count (Alertmanager `/api/v2/alerts` shape).
  - one matching `system*` → status grid: three lights (running · backed up · up to date) per service, four services across, with a detail line under the grid for anything failing.
  - one matching `update*` → footer count of `updateAvailable=true` entries (WUD-shaped JSON).
  - one matching `kuma*` → [uptime graph](#uptime-graphs) (binary up/down bar, 24h).
  - any matching `prometheus*` → [uptime graph](#uptime-graphs) (value + sparkline).
- `${VAR}` substitution in URLs/headers reads from the process env, optionally seeded from a `.env` file (`--env`).

What it does **not** reuse: Go HTML templates from `custom-api` widgets, the `weather` widget, anything that requires a browser DOM.

## Install

```sh
go install github.com/kjaymiller/glancectl/cmd/glancectl@latest
```

Or build from source:

```sh
git clone https://github.com/kjaymiller/glancectl
cd glancectl
go build -o glancectl ./cmd/glancectl
```

## Run

```sh
glancectl
```

With no flags, glancectl finds its config by convention, first match wins:

1. `$GLANCECTL_CONFIG_PATH` — a file, or a directory holding `glance.yml`
2. `./configs/glance/glance.yml`, then `./glance.yml`
3. `$XDG_CONFIG_HOME/glancectl/`, then `~/.config/glancectl/`
4. `~/.config/glance/` — your existing Glance install, used as-is

Within a directory it accepts `glance.yml`, `glance.yaml`, `config.yml`, or
`config.yaml`. If nothing is found, glancectl prints the paths it searched.

The dotenv file is optional and resolved the same way: `$GLANCECTL_ENV_PATH`,
then `.env` beside the config, then `./compose/glance/.env`, then `.env` in the
config directories above.

Flags (each overrides the discovery above):

| flag | default | meaning |
|---|---|---|
| `--config` | discovered | path to glance.yml |
| `--env` | discovered | dotenv file for `${VAR}` substitution |
| `--rewrite` | `$GLANCECTL_REWRITE` | rewrite unreachable hosts (see below) |
| `--workdir` | `.` | where to run `just` recipes |
| `--refresh` | `30s` | health/counts refresh interval |
| `--version` | | print version |

## Keys

| key | action |
|---|---|
| `tab` / `shift+tab` | switch pane |
| `↑`/`↓` or `k`/`j` | move cursor (Actions/Bookmarks) · scroll (Feed) |
| `enter` | activate (Actions: run `just <recipe>` · Bookmarks: open URL) |
| `y` | copy the focused pane as plain text (see below) |
| `r` | refresh now |
| `esc` | clear runner output / status line |
| `q` / `ctrl+c` | quit |

## Uptime graphs

Two card types, split by what each backend is actually good at: Kuma answers
*was it up*, Prometheus answers everything else. Both are configured entirely
through the Glance config, using only keys a real Glance instance accepts — so
the same file keeps working if Glance is reading it too.

### Kuma — binary 24h bar

A `custom-api` widget whose title contains `kuma`. One row per monitor: name,
a 32-column bar covering the last 24 hours, and Kuma's own 24h uptime figure.

```yaml
- type: custom-api
  title: Kuma Uptime
  cache: 1m
  url: https://kuma.kjaymiller.dev/status/homelab
  template: "{{ .JSON.String \"\" }}"   # ignored by glancectl; keeps Glance happy
```

The `url` may be the status page (`/status/<slug>`), the heartbeat endpoint
(`/api/status-page/heartbeat/<slug>`), or the status-page API path — all three
resolve to the same slug.

Each column is 45 minutes and takes the **worst** status in it, so a single
failed check still shows. Glyphs carry the signal so the bar stays readable
without colour:

| glyph | meaning |
|---|---|
| `█` | up |
| `▁` | down |
| `▄` | pending |
| `▒` | maintenance |
| `·` | no heartbeat in that bucket |

Gaps are *not* counted as uptime — a monitor added an hour ago shows 23 hours
of `·`, not a full green day.

**How much of the bar fills.** Kuma's status page API returns the last **100
heartbeats** per monitor, not 24 hours of them. At the usual 60s interval that
is ~100 minutes, so most of the bar is `·` and only the right-hand end is
filled. The trailing `99.93%` is Kuma's own 24h figure and *is* accurate over
the full day — so a monitor can legitimately show an all-green bar next to a
sub-100% number, meaning the dip happened before the last 100 beats. Raise the
monitor's `interval` if you want the bar to span more wall-clock time.

> **Requires a public status page.** Kuma exposes heartbeat history only
> through a status page, and answers with an empty list (not a 404) when the
> slug has none — so glancectl reports that case as an error rather than as
> "all green". Create one in Kuma under **Status Pages → New Status Page**, add
> the monitors, and make it public. `/metrics` is not an alternative: it needs
> an API key and carries only current state, no history.

### Prometheus — value + sparkline

A `custom-api` widget whose title contains `prometheus`. The PromQL and display
hints ride in `parameters`, which Glance treats as ordinary query params:

```yaml
- type: custom-api
  title: Prometheus CPU
  cache: 1m
  url: https://prometheus.kjaymiller.dev/api/v1/query
  parameters:
    query: 100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)
    label: CPU busy
    format: percent
    range: 24h
    step: 15m
```

| parameter | default | meaning |
|---|---|---|
| `query` | — | PromQL (required) |
| `range` | `24h` | lookback window (Go duration) |
| `step` | `15m` | sample interval (Go duration) |
| `label` | the query | display name |
| `unit` | — | suffix for the current value |
| `format` | — | `percent`, `bytes`, or `duration` |

Renders as the current value, a sparkline over the window, and the window's low
and high. One query per widget — add a widget per metric:

```
Prometheus CPU
  3.1%  CPU busy
  ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▂▃▁▁▁▁▁▁▁▁▁▁▁▁▁▁▃▆▄▄▇
  24h · low 1.9% · high 3.4%
```

`url` may be the host, `/api/v1/query`, or `/api/v1/query_range` — all are
normalised to the range endpoint. A query matching several series uses the
first; aggregate in the query (`avg(...)`, `sum(...)`) to control which. `NaN`
and `±Inf` samples are dropped rather than plotted at the axis.

Both card types honour the host rewriting below.

## Reaching container-only services

Glance runs inside the compose network and addresses services by their
Docker-network names (`http://update-shim:5000`). Those do not resolve on the
host, so glancectl shows a DNS error for those widgets. Rather than forking the
shared config, rewrite the unreachable hosts at fetch time:

```sh
export GLANCECTL_REWRITE='
  update-shim:5000=https://update.kjaymiller.dev,
  alertmanager:9093=https://alertmanager.kjaymiller.dev
'
```

Rules are `host[:port]=replacement`, comma- or newline-separated, and may also
be passed with `--rewrite`. A rule with a port only matches that port; one
without matches any port on that host. The replacement may be a bare authority
(keeps the original scheme) or a full URL (its scheme wins, and any path on it
is prefixed). Paths and query strings are always preserved:

    http://update-shim:5000/api/containers
      → https://update.kjaymiller.dev/api/containers

Rewrites apply to widget sources, monitor sites, and bookmarks alike, so the
URL shown in a copied pane is the one actually fetched. A URL that matches no
rule is left untouched.

## Copying a pane

The dashboard runs in the alt-screen, where terminal mouse selection is
unreliable — so an error shown in a card can be hard to get out. `y` copies the
focused pane as plain text, rebuilt from the underlying data rather than scraped
off the rendered box: no ANSI escapes, no hard wrapping, and with detail the
compact view omits (service URLs, HTTP status codes, the widget's source URL).

    Systems
      source: http://service-status:5000/services.json
      get http://service-status:5000/services.json: 500 Internal Server Error

It uses the first of `wl-copy`, `xclip`, `xsel`, or `pbcopy` that is installed.
With none of them it falls back to OSC 52, which works over SSH but which some
terminals disable — since that path gives no confirmation, the text is also
written to a temp file and the footer reports the path.

## License

MIT.
