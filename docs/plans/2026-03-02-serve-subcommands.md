# Serve Start/Stop/Restart Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace `--install`/`--uninstall` flags on `tsp serve` with `start`, `stop`, and `restart` subcommands for daemon lifecycle management.

**Architecture:** Refactor `launchd.go` into reusable primitives (`ensurePlist`, `loadService`, `unloadService`, `isServiceLoaded`), then wire three new cobra subcommands that compose those primitives. Remove the old flags from `serve.go`.

**Tech Stack:** Go, cobra, launchctl, macOS LaunchAgents

---

### Task 1: Refactor launchd.go into reusable primitives

**Files:**
- Modify: `internal/cmd/launchd.go`

**Step 1: Rewrite `launchd.go`**

Replace `installLaunchd()` and `uninstallLaunchd()` with four focused functions:

```go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const plistLabel = "com.tsp.serve"

var plistTemplate = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.Binary}}</string>
        <string>serve</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>{{.Path}}</string>
        <key>HOME</key>
        <string>{{.Home}}</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{.LogDir}}/serve.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogDir}}/serve.log</string>
</dict>
</plist>
`))

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", plistLabel+".plist")
}

func logDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".tsp")
	os.MkdirAll(dir, 0755)
	return dir
}

// ensurePlist writes the launchd plist file if it doesn't already exist.
// Returns the plist path and any error.
func ensurePlist() (string, error) {
	path := plistPath()
	if _, err := os.Stat(path); err == nil {
		return path, nil // already exists
	}

	binary, err := exec.LookPath("tsp")
	if err != nil {
		binary, _ = os.Executable()
	}

	home, _ := os.UserHomeDir()

	data := struct {
		Label, Binary, LogDir, Path, Home string
	}{
		Label:  plistLabel,
		Binary: binary,
		LogDir: logDir(),
		Path:   os.Getenv("PATH"),
		Home:   home,
	}

	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("creating plist: %w", err)
	}
	defer f.Close()

	if err := plistTemplate.Execute(f, data); err != nil {
		return "", fmt.Errorf("writing plist: %w", err)
	}

	return path, nil
}

// isServiceLoaded checks whether the launchd service is currently loaded.
func isServiceLoaded() bool {
	return exec.Command("launchctl", "list", plistLabel).Run() == nil
}

// loadService loads the launchd plist via launchctl.
func loadService(plistPath string) error {
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}
	return nil
}

// unloadService unloads the launchd plist via launchctl.
func unloadService() error {
	path := plistPath()
	if err := exec.Command("launchctl", "unload", path).Run(); err != nil {
		return fmt.Errorf("launchctl unload: %w", err)
	}
	return nil
}
```

**Step 2: Verify it compiles**

Run: `cd /Users/mhdev/code/tmux-super-powers && go build ./...`
Expected: compiles cleanly (the old callers are removed in a later task)

**Step 3: Commit**

```bash
git add internal/cmd/launchd.go
git commit -m "refactor: extract reusable launchd primitives from launchd.go"
```

---

### Task 2: Create `tsp serve start` subcommand

**Files:**
- Create: `internal/cmd/serve_start.go`

**Step 1: Write serve_start.go**

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var serveStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the tsp server as a background daemon",
	Long: `Install the launchd plist (if needed) and start the tsp server daemon.

The daemon runs in the background and auto-starts on login.
Use 'tsp serve stop' to stop it, 'tsp serve status' to check health.`,
	Run: func(cmd *cobra.Command, args []string) {
		if isServiceLoaded() {
			fmt.Println("tsp server is already running.")
			fmt.Println("Use 'tsp serve restart' to restart it.")
			return
		}

		path, err := ensurePlist()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if err := loadService(path); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("tsp server started.")
		fmt.Printf("  Plist: %s\n", path)
		fmt.Printf("  Logs:  %s/serve.log\n", logDir())
	},
}
```

**Step 2: Verify it compiles**

Run: `cd /Users/mhdev/code/tmux-super-powers && go build ./...`
Expected: compiles (command not yet registered, but file is valid)

**Step 3: Commit**

```bash
git add internal/cmd/serve_start.go
git commit -m "feat: add 'tsp serve start' subcommand"
```

---

### Task 3: Create `tsp serve stop` subcommand

**Files:**
- Create: `internal/cmd/serve_stop.go`

**Step 1: Write serve_stop.go**

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var serveStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the tsp server daemon",
	Long: `Stop the running tsp server daemon via launchctl.

The launchd plist is kept in place so 'tsp serve start' can reload it.`,
	Run: func(cmd *cobra.Command, args []string) {
		if !isServiceLoaded() {
			fmt.Println("tsp server is not running.")
			return
		}

		if err := unloadService(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("tsp server stopped.")
	},
}
```

**Step 2: Verify it compiles**

Run: `cd /Users/mhdev/code/tmux-super-powers && go build ./...`
Expected: compiles cleanly

**Step 3: Commit**

```bash
git add internal/cmd/serve_stop.go
git commit -m "feat: add 'tsp serve stop' subcommand"
```

---

### Task 4: Create `tsp serve restart` subcommand

**Files:**
- Create: `internal/cmd/serve_restart.go`

**Step 1: Write serve_restart.go**

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var serveRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the tsp server daemon",
	Long:  `Stop and start the tsp server daemon. Installs the plist if missing.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Stop if running (ignore error if not loaded)
		if isServiceLoaded() {
			_ = unloadService()
		}

		path, err := ensurePlist()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if err := loadService(path); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("tsp server restarted.")
		fmt.Printf("  Logs: %s/serve.log\n", logDir())
	},
}
```

**Step 2: Verify it compiles**

Run: `cd /Users/mhdev/code/tmux-super-powers && go build ./...`
Expected: compiles cleanly

**Step 3: Commit**

```bash
git add internal/cmd/serve_restart.go
git commit -m "feat: add 'tsp serve restart' subcommand"
```

---

### Task 5: Wire subcommands into serve.go, remove old flags

**Files:**
- Modify: `internal/cmd/serve.go`

**Step 1: Update serve.go**

Remove `--install`/`--uninstall` flags and their handler code. Register the three new subcommands. Update the help text.

The `serveCmd` variable becomes:

```go
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the API server for mobile app access",
	Long: `Start an HTTP/WebSocket API server that exposes tsp functionality.

The server binds to your Tailscale IP by default (100.x.x.x range).
If Tailscale is not detected, falls back to localhost.

Examples:
  tsp serve              # Start in foreground on default port (7777)
  tsp serve --port 8080  # Custom port
  tsp serve --bind 0.0.0.0  # Override bind address

Daemon management:
  tsp serve start        # Start as background daemon (launchd)
  tsp serve stop         # Stop the daemon
  tsp serve restart      # Restart the daemon
  tsp serve status       # Check daemon health`,
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetInt("port")
		bind, _ := cmd.Flags().GetString("bind")

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		if port == 0 {
			port = cfg.Serve.Port
		}

		bindAddr := server.ResolveBindAddress(bind)

		srv, err := server.New(cfg, config.TspDir())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initialising server: %v\n", err)
			os.Exit(1)
		}

		// Graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			log.Println("Shutting down...")
			srv.Stop()
		}()

		if err := srv.Start(bindAddr, port); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	},
}
```

The `init()` function becomes:

```go
func init() {
	serveCmd.Flags().IntP("port", "p", 0, "Port to listen on (default: from config or 7777)")
	serveCmd.Flags().String("bind", "", "Address to bind to (default: Tailscale IP or 127.0.0.1)")
	serveCmd.AddCommand(serveStatusCmd)
	serveCmd.AddCommand(serveStartCmd)
	serveCmd.AddCommand(serveStopCmd)
	serveCmd.AddCommand(serveRestartCmd)
}
```

**Step 2: Update serve_status.go hint text**

In `serve_status.go:37`, change:
```go
fmt.Println("  Daemon installed:  no (run `tsp serve --install`)")
```
to:
```go
fmt.Println("  Daemon installed:  no (run `tsp serve start`)")
```

**Step 3: Verify it compiles**

Run: `cd /Users/mhdev/code/tmux-super-powers && go build ./...`
Expected: compiles cleanly

**Step 4: Run tests**

Run: `cd /Users/mhdev/code/tmux-super-powers && go test ./internal/cmd/...`
Expected: all tests pass (the `parseLaunchctlPID` test is unaffected)

**Step 5: Commit**

```bash
git add internal/cmd/serve.go internal/cmd/serve_status.go
git commit -m "feat: wire start/stop/restart subcommands, remove --install/--uninstall flags"
```
