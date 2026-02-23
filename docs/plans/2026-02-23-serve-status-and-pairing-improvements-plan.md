# Serve Status & Pairing Improvements Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `tsp serve status` to check daemon health, change pairing QR to JSON payload with server address, and add `--address` flag to `tsp device pair`.

**Architecture:** Three changes to existing CLI commands. Status uses launchd + HTTP health check. QR payload switches from URL to JSON. Address flag with smart normalization feeds into QR JSON.

**Tech Stack:** Go, cobra, os/exec (launchctl), net/http, encoding/json, go-qrcode

---

### Task 1: Address Normalization Helper

**Files:**
- Create: `internal/cmd/address.go`
- Create: `internal/cmd/address_test.go`

**Step 1: Write the failing test**

In `internal/cmd/address_test.go`:

```go
package cmd

import "testing"

func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		input    string
		port     int
		expected string
	}{
		// Bare hostname → prepend http://, append port
		{"my-machine.tail1234.ts.net", 7777, "http://my-machine.tail1234.ts.net:7777"},
		// Hostname with port → prepend http://, keep port
		{"my-machine.tail1234.ts.net:8080", 7777, "http://my-machine.tail1234.ts.net:8080"},
		// Full URL → use as-is
		{"http://10.0.0.1:7777", 7777, "http://10.0.0.1:7777"},
		// Full URL with different port → use as-is
		{"http://10.0.0.1:9999", 7777, "http://10.0.0.1:9999"},
		// IP only → prepend http://, append port
		{"100.68.1.42", 7777, "http://100.68.1.42:7777"},
		// IP with port → prepend http://, keep port
		{"100.68.1.42:8080", 7777, "http://100.68.1.42:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeAddress(tt.input, tt.port)
			if result != tt.expected {
				t.Errorf("normalizeAddress(%q, %d) = %q, want %q", tt.input, tt.port, result, tt.expected)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cmd/ -run TestNormalizeAddress -v`
Expected: FAIL — `normalizeAddress` undefined

**Step 3: Write minimal implementation**

In `internal/cmd/address.go`:

```go
package cmd

import (
	"fmt"
	"strings"
)

// normalizeAddress takes a user-provided address and ensures it's a full URL.
// Rules:
//   - "http://..." → use as-is
//   - "host:port" → prepend http://
//   - "host" → prepend http://, append default port
func normalizeAddress(addr string, defaultPort int) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	if strings.Contains(addr, ":") {
		return "http://" + addr
	}
	return fmt.Sprintf("http://%s:%d", addr, defaultPort)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cmd/ -run TestNormalizeAddress -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cmd/address.go internal/cmd/address_test.go
git commit -m "feat: add address normalization helper for pairing"
```

---

### Task 2: QR Code JSON Payload + --address Flag

**Files:**
- Modify: `internal/cmd/device.go` (runDevicePair function, init function)

**Step 1: Add `--address` flag to init()**

In `internal/cmd/device.go`, update the `init()` function — add the address flag to `devicePairCmd`:

```go
devicePairCmd.Flags().String("address", "", "Override server address in QR code (e.g. my-machine.tail1234.ts.net)")
```

**Step 2: Update `runDevicePair` to use JSON QR and --address flag**

Replace the QR code generation section in `runDevicePair` (lines 133–156). After the `initiateResp` is decoded:

```go
	code := initiateResp.Code
	address := initiateResp.Address

	// --address flag overrides server-reported address
	addrFlag, _ := cmd.Flags().GetString("address")
	if addrFlag != "" {
		address = normalizeAddress(addrFlag, port)
	} else if address == "" {
		address = fmt.Sprintf("http://127.0.0.1:%d", port)
	} else if !strings.HasPrefix(address, "http") {
		address = fmt.Sprintf("http://%s", address)
	}

	// Build JSON payload for QR code
	qrPayload := fmt.Sprintf(`{"address":%q,"code":%q}`, address, code)

	// Render QR code in terminal
	qr, err := qrcode.New(qrPayload, qrcode.Medium)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating QR code: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("Scan this QR code with the tsp mobile app:")
	fmt.Println()
	fmt.Print(qr.ToSmallString(false))
	fmt.Println()
	fmt.Printf("Server:  %s\n", address)
	fmt.Printf("Code:    %s\n", code)
	fmt.Println()
	fmt.Println("Waiting for device to pair...")
```

**Step 3: Run existing tests to verify nothing breaks**

Run: `go test ./internal/... -v`
Expected: All existing tests PASS

**Step 4: Build and verify**

Run: `go build -o tsp ./cmd/tsp && ./tsp device pair --help`
Expected: Output shows `--address` flag in help text

**Step 5: Commit**

```bash
git add internal/cmd/device.go
git commit -m "feat: QR code uses JSON payload with --address override"
```

---

### Task 3: `tsp serve status` Subcommand

**Files:**
- Create: `internal/cmd/serve_status.go`
- Create: `internal/cmd/serve_status_test.go`
- Modify: `internal/cmd/serve.go` (add subcommand registration)

**Step 1: Write the failing test for status check helpers**

In `internal/cmd/serve_status_test.go`:

```go
package cmd

import "testing"

func TestParseLaunchctlPID(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
	}{
		{
			name:     "running with PID",
			output:   "{\n\t\"LimitLoadToSessionType\" = \"Aqua\";\n\t\"Label\" = \"com.tsp.serve\";\n\t\"OnDemand\" = false;\n\t\"LastExitStatus\" = 0;\n\t\"PID\" = 12345;\n\t\"Program\" = \"/usr/local/bin/tsp\";\n};",
			expected: "12345",
		},
		{
			name:     "not running no PID",
			output:   "{\n\t\"LimitLoadToSessionType\" = \"Aqua\";\n\t\"Label\" = \"com.tsp.serve\";\n\t\"OnDemand\" = false;\n\t\"LastExitStatus\" = 256;\n};",
			expected: "",
		},
		{
			name:     "empty output",
			output:   "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pid := parseLaunchctlPID(tt.output)
			if pid != tt.expected {
				t.Errorf("parseLaunchctlPID() = %q, want %q", pid, tt.expected)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cmd/ -run TestParseLaunchctlPID -v`
Expected: FAIL — `parseLaunchctlPID` undefined

**Step 3: Implement serve_status.go**

In `internal/cmd/serve_status.go`:

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/spf13/cobra"
)

var serveStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check if the tsp server is running",
	Long:  `Check daemon installation, process status, and server health.`,
	Run:   runServeStatus,
}

func runServeStatus(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	port := cfg.Serve.Port

	fmt.Println("tsp serve status:")

	// 1. Check plist exists
	path := plistPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Println("  Daemon installed:  no (run `tsp serve --install`)")
		fmt.Println("  Process running:   -")
		fmt.Println("  Server responding: -")
		return
	}
	fmt.Printf("  Daemon installed:  yes (%s)\n", path)

	// 2. Check launchctl for PID
	out, err := exec.Command("launchctl", "list", plistLabel).Output()
	if err != nil {
		fmt.Println("  Process running:   no (not loaded)")
		fmt.Println("  Server responding: -")
		return
	}
	pid := parseLaunchctlPID(string(out))
	if pid != "" {
		fmt.Printf("  Process running:   yes (PID %s)\n", pid)
	} else {
		fmt.Println("  Process running:   no (check ~/.tsp/serve.log)")
	}

	// 3. Check HTTP health
	client := &http.Client{Timeout: 2 * time.Second}
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/api/health", port)
	resp, err := client.Get(healthURL)
	if err != nil {
		fmt.Printf("  Server responding: no (port %d not reachable)\n", port)
		return
	}
	defer resp.Body.Close()

	var health struct {
		Tmux bool   `json:"tmux"`
		Gh   bool   `json:"gh"`
		Time string `json:"time"`
	}
	json.NewDecoder(resp.Body).Decode(&health)
	fmt.Printf("  Server responding: yes (http://127.0.0.1:%d)\n", port)
}

var pidRegexp = regexp.MustCompile(`"PID"\s*=\s*(\d+)`)

func parseLaunchctlPID(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	matches := pidRegexp.FindStringSubmatch(output)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cmd/ -run TestParseLaunchctlPID -v`
Expected: PASS

**Step 5: Register subcommand**

In `internal/cmd/serve.go`, add to the `init()` function:

```go
serveCmd.AddCommand(serveStatusCmd)
```

**Step 6: Run all tests**

Run: `go test ./... -v`
Expected: All PASS

**Step 7: Build and verify**

Run: `go build -o tsp ./cmd/tsp && ./tsp serve status`
Expected: Shows status output

**Step 8: Commit**

```bash
git add internal/cmd/serve_status.go internal/cmd/serve_status_test.go internal/cmd/serve.go
git commit -m "feat: add tsp serve status command"
```

---

### Task 4: Final Integration Verification

**Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: All PASS

**Step 2: Build clean binary**

Run: `go build -o tsp ./cmd/tsp`

**Step 3: Verify help output**

Run: `./tsp serve --help` — should show `status` subcommand
Run: `./tsp device pair --help` — should show `--address` flag

**Step 4: Commit if any fixes needed, otherwise done**
