# glancectl

A terminal dashboard that reads the same [Glance](https://github.com/glanceapp/glance) config you already have. Three panes: live service health, [`just`](https://github.com/casey/just) recipes you can run, and bookmarks you can launch.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## What it reuses from Glance

- `monitor` widgets → **Services** pane (live HTTP probe, ✓/✗/status code).
- `bookmarks` widgets → **Bookmarks** pane (`enter` opens in `$BROWSER`).
- `custom-api` widgets, by title:
  - one matching `alert*` → footer count of active alerts (Alertmanager `/api/v2/alerts` shape).
  - one matching `update*` → footer count of `updateAvailable=true` entries (WUD-shaped JSON).
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

From the directory holding your `configs/glance/glance.yml`:

```sh
glancectl
```

Flags:

| flag | default | meaning |
|---|---|---|
| `--config` | `configs/glance/glance.yml` | path to glance.yml |
| `--env` | `compose/glance/.env` | dotenv file for `${VAR}` substitution |
| `--workdir` | `.` | where to run `just` recipes |
| `--refresh` | `30s` | health/counts refresh interval |
| `--version` | | print version |

## Keys

| key | action |
|---|---|
| `tab` / `shift+tab` | switch pane |
| `↑`/`↓` or `k`/`j` | move cursor |
| `enter` | activate (Services: open URL · Actions: run `just <recipe>` · Bookmarks: open URL) |
| `r` | refresh now |
| `esc` | clear runner output |
| `q` / `ctrl+c` | quit |

## License

MIT.
