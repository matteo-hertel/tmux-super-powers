package cmd

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/server"
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

	// 3. Check HTTP health — try localhost first, then Tailscale IP
	client := &http.Client{Timeout: 2 * time.Second}
	addresses := []string{"127.0.0.1"}
	if tsIP := server.DetectTailscaleIP(); tsIP != "" {
		addresses = append(addresses, tsIP)
	}

	for _, addr := range addresses {
		healthURL := fmt.Sprintf("http://%s:%d/api/health", addr, port)
		resp, err := client.Get(healthURL)
		if err != nil {
			continue
		}
		resp.Body.Close()
		fmt.Printf("  Server responding: yes (http://%s:%d)\n", addr, port)
		return
	}
	fmt.Printf("  Server responding: no (port %d not reachable)\n", port)
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
