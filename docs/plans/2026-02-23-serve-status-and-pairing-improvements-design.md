# Serve Status & Pairing Improvements Design

**Date**: 2026-02-23

## Problems

1. No way to check if `tsp serve` daemon is running
2. QR code during pairing only contains a completion URL — phone app needs the server address for future API calls
3. No way to override the address in the QR code (needed for Tailscale Magic DNS, multiple Tailscale profiles, or when Tailscale isn't running at daemon start)

## Design

### 1. `tsp serve status` Command

New subcommand that performs three checks and reports results.

**Output (all healthy):**
```
tsp serve status:
  Daemon installed:  yes (~/Library/LaunchAgents/com.tsp.serve.plist)
  Process running:   yes (PID 12345)
  Server responding: yes (http://100.68.1.42:7777)
```

**Logic:**
1. Check if plist file exists at `~/Library/LaunchAgents/com.tsp.serve.plist`
2. Run `launchctl list com.tsp.serve` — if exit code 0, extract PID from output
3. Hit `GET http://127.0.0.1:{port}/api/health` with a 2-second timeout
4. If health responds, show the bound address

**Failure messages:**
- Plist missing → `Daemon installed: no (run tsp serve --install)`
- Process not running → `Process running: no (check ~/.tsp/serve.log)`
- Server not responding → `Server responding: no (port {port} not reachable)`

**Implementation:** Add `statusCmd` as a subcommand of `serveCmd` in `internal/cmd/serve.go`.

### 2. QR Code JSON Payload

Change QR content from a URL to a JSON object:

**Before:** `http://100.68.1.42:7777/api/pair/complete?code=ABC123`

**After:**
```json
{
  "address": "http://100.68.1.42:7777",
  "code": "ABC123"
}
```

The phone app parses this JSON, stores the address for future API calls, and uses the code to complete pairing.

### 3. `--address` Flag on `tsp device pair`

New flag to override the server address in the QR code.

**Usage:**
```
tsp device pair --address my-machine.tail1234.ts.net
tsp device pair --address my-machine.tail1234.ts.net:8080
tsp device pair --address http://10.0.0.1:7777
```

**Address resolution order:**
1. `--address` flag (if provided)
2. Server's reported address from `/api/pair/initiate` response
3. Fallback to `127.0.0.1:{port}`

**Smart address normalization:**
- `hostname` → `http://hostname:7777` (prepend scheme, append default port)
- `hostname:8080` → `http://hostname:8080` (prepend scheme, use provided port)
- `http://10.0.0.1:7777` → used as-is (full URL provided)

**Updated terminal output:**
```
Scan this QR code with the tsp mobile app:

  [QR CODE]

Server:  http://macbook.tail1234.ts.net:7777
Code:    ABC123

Waiting for device to pair...
```

### Files to Modify

- `internal/cmd/serve.go` — add status subcommand
- `internal/cmd/device.go` — change QR content to JSON, add `--address` flag, add address normalization, update terminal output

### Out of Scope

- CLI commands silently doing nothing — separate debugging session needed
- Config-based address override — can be added later if the flag proves insufficient
