package cmd

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/server"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the API server for mobile app access",
	Long: `Start an HTTP/WebSocket API server that exposes tsp functionality.

The server binds to your Tailscale IP by default (100.x.x.x range).
If Tailscale is not detected, falls back to localhost.

Examples:
  tsp serve              # Start on default port (7777)
  tsp serve --port 8080  # Custom port
  tsp serve --bind 0.0.0.0  # Override bind address

Daemon management:
  tsp serve --install    # Install as launchd service (auto-start on login)
  tsp serve --uninstall  # Remove launchd service`,
	Run: func(cmd *cobra.Command, args []string) {
		install, _ := cmd.Flags().GetBool("install")
		uninstall, _ := cmd.Flags().GetBool("uninstall")

		if install {
			installLaunchd()
			return
		}
		if uninstall {
			uninstallLaunchd()
			return
		}

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

func init() {
	serveCmd.Flags().IntP("port", "p", 0, "Port to listen on (default: from config or 7777)")
	serveCmd.Flags().String("bind", "", "Address to bind to (default: Tailscale IP or 127.0.0.1)")
	serveCmd.Flags().Bool("install", false, "Install as launchd service")
	serveCmd.Flags().Bool("uninstall", false, "Remove launchd service")
	serveCmd.AddCommand(serveStatusCmd)
}
