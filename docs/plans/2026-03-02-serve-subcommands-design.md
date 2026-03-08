# Design: `tsp serve start|stop|restart`

Replace `--install`/`--uninstall` flags with proper subcommands for daemon lifecycle management.

## Commands

| Command | Behavior |
|---------|----------|
| `tsp serve` | Run server in foreground (unchanged) |
| `tsp serve start` | Write plist if missing, `launchctl load`. Report if already running. |
| `tsp serve stop` | `launchctl unload`. Leave plist in place. Report if not running. |
| `tsp serve restart` | Unload then load. Write plist if missing. |
| `tsp serve status` | Unchanged |

## Removed

- `--install` flag (replaced by `start`)
- `--uninstall` flag (replaced by `stop`)

## Key decisions

- `stop` leaves the plist so `start` can reload without reinstalling.
- `start` is idempotent: if the plist exists and service is loaded, it reports that.
- `restart` is `stop` + `start`, tolerating the case where the service isn't currently running.

## Files

- `internal/cmd/serve.go` — remove `--install`/`--uninstall`, register new subcommands
- `internal/cmd/serve_start.go` — new, `start` subcommand
- `internal/cmd/serve_stop.go` — new, `stop` subcommand
- `internal/cmd/serve_restart.go` — new, `restart` subcommand
- `internal/cmd/launchd.go` — refactor into reusable functions: `ensurePlist()`, `loadService()`, `unloadService()`, `isServiceLoaded()`
