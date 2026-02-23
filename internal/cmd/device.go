package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/device"
	"github.com/matteo-hertel/tmux-super-powers/internal/server"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"
)

var deviceCmd = &cobra.Command{
	Use:   "device",
	Short: "Manage paired devices",
	Long:  `Manage paired mobile devices: pair new devices, list existing ones, or revoke access.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var devicePairCmd = &cobra.Command{
	Use:   "pair",
	Short: "Pair a new device via QR code",
	Long: `Initiate device pairing by displaying a QR code in the terminal.
Scan the QR code from the mobile app, or enter the short code manually.
The command polls the server until the device is paired or 5 minutes elapse.

Examples:
  tsp device pair
  tsp device pair --name "My iPhone"`,
	Run: runDevicePair,
}

var deviceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List paired devices",
	Long:  `Display a table of all paired devices with their ID, name, pairing date, and last seen time.`,
	Run:   runDeviceList,
}

var deviceRevokeCmd = &cobra.Command{
	Use:   "revoke <id|name>",
	Short: "Revoke a paired device",
	Long: `Revoke access for a paired device by its ID or name.
The device will no longer be able to authenticate with the server.

Examples:
  tsp device revoke d_abc123
  tsp device revoke "My iPhone"`,
	Args: cobra.ExactArgs(1),
	Run:  runDeviceRevoke,
}

func init() {
	devicePairCmd.Flags().String("name", "", "Name for the device being paired")
	devicePairCmd.Flags().String("address", "", "Override server address in QR code (e.g. my-machine.tail1234.ts.net)")
	deviceCmd.AddCommand(devicePairCmd)
	deviceCmd.AddCommand(deviceListCmd)
	deviceCmd.AddCommand(deviceRevokeCmd)
}

func runDevicePair(cmd *cobra.Command, args []string) {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		name = "unnamed device"
	}

	// Read admin token
	adminTokenPath := filepath.Join(config.TspDir(), "admin-token")
	tokenData, err := os.ReadFile(adminTokenPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading admin token from %s: %v\n", adminTokenPath, err)
		fmt.Fprintln(os.Stderr, "Make sure the server has been started at least once (tsp serve).")
		os.Exit(1)
	}
	adminToken := strings.TrimSpace(string(tokenData))
	if adminToken == "" {
		fmt.Fprintln(os.Stderr, "Admin token file is empty. Start the server first (tsp serve).")
		os.Exit(1)
	}

	// Load config for server port
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	port := cfg.Serve.Port

	// Try localhost first, then Tailscale IP (same logic as serve status)
	addresses := []string{"127.0.0.1"}
	if tsIP := server.DetectTailscaleIP(); tsIP != "" {
		addresses = append(addresses, tsIP)
	}

	body := fmt.Sprintf(`{"name":%q}`, name)
	var baseURL string
	var resp *http.Response
	for _, addr := range addresses {
		baseURL = fmt.Sprintf("http://%s:%d", addr, port)
		initiateURL := baseURL + "/api/pair/initiate"
		req, err := http.NewRequest("POST", initiateURL, strings.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp, err = (&http.Client{Timeout: 2 * time.Second}).Do(req)
		if err == nil {
			break
		}
	}
	if resp == nil {
		fmt.Fprintf(os.Stderr, "Error connecting to server on port %d\n", port)
		fmt.Fprintln(os.Stderr, "Is the server running? Start it with: tsp serve")
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Server error (%d): %s\n", resp.StatusCode, string(respBody))
		os.Exit(1)
	}

	var initiateResp struct {
		Code    string `json:"code"`
		Address string `json:"address"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&initiateResp); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing server response: %v\n", err)
		os.Exit(1)
	}

	code := initiateResp.Code
	address := initiateResp.Address

	// --address flag overrides server-reported address
	addrFlag, _ := cmd.Flags().GetString("address")
	if addrFlag != "" {
		address = normalizeAddress(addrFlag, port)
	} else if address == "" {
		address = fmt.Sprintf("http://127.0.0.1:%d", port)
	} else if !strings.HasPrefix(address, "http") {
		address = normalizeAddress(address, port)
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

	// Poll for pairing status
	statusURL := fmt.Sprintf("%s/api/pair/status?code=%s", baseURL, code)
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			fmt.Fprintln(os.Stderr, "\nPairing timed out after 5 minutes.")
			os.Exit(1)
		case <-ticker.C:
			fmt.Print(".")

			statusReq, err := http.NewRequest("GET", statusURL, nil)
			if err != nil {
				continue
			}
			statusReq.Header.Set("Authorization", "Bearer "+adminToken)

			statusResp, err := http.DefaultClient.Do(statusReq)
			if err != nil {
				continue
			}

			var statusBody struct {
				Claimed    bool   `json:"claimed"`
				DeviceName string `json:"device_name"`
			}
			json.NewDecoder(statusResp.Body).Decode(&statusBody)
			statusResp.Body.Close()

			if statusBody.Claimed {
				fmt.Printf("\nDevice '%s' paired successfully!\n", statusBody.DeviceName)
				return
			}
		}
	}
}

func runDeviceList(cmd *cobra.Command, args []string) {
	storePath := filepath.Join(config.TspDir(), "devices.json")
	store := device.NewStore(storePath)

	devices, err := store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading devices: %v\n", err)
		os.Exit(1)
	}

	if len(devices) == 0 {
		fmt.Println("No paired devices")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPAIRED AT\tLAST SEEN")
	fmt.Fprintln(w, "--\t----\t---------\t---------")

	for _, d := range devices {
		pairedAt := d.PairedAt.Local().Format("2006-01-02 15:04")
		lastSeen := "-"
		if !d.LastSeen.IsZero() {
			lastSeen = d.LastSeen.Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", d.ID, d.Name, pairedAt, lastSeen)
	}
	w.Flush()
}

func runDeviceRevoke(cmd *cobra.Command, args []string) {
	target := args[0]

	storePath := filepath.Join(config.TspDir(), "devices.json")
	store := device.NewStore(storePath)

	devices, err := store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading devices: %v\n", err)
		os.Exit(1)
	}

	// Match by ID first
	var matchedID string
	var matchedName string
	for _, d := range devices {
		if d.ID == target {
			matchedID = d.ID
			matchedName = d.Name
			break
		}
	}

	// If no ID match, try case-insensitive name match
	if matchedID == "" {
		for _, d := range devices {
			if strings.EqualFold(d.Name, target) {
				matchedID = d.ID
				matchedName = d.Name
				break
			}
		}
	}

	if matchedID == "" {
		fmt.Fprintf(os.Stderr, "No device found matching '%s'\n", target)
		os.Exit(1)
	}

	if err := store.Remove(matchedID); err != nil {
		fmt.Fprintf(os.Stderr, "Error revoking device: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Revoked device '%s' (%s)\n", matchedName, matchedID)
}
