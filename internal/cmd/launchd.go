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

func installLaunchd() {
	binary, err := exec.LookPath("tsp")
	if err != nil {
		// Fall back to current executable
		binary, _ = os.Executable()
	}

	data := struct {
		Label  string
		Binary string
		LogDir string
	}{
		Label:  plistLabel,
		Binary: binary,
		LogDir: logDir(),
	}

	path := plistPath()
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating plist: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	if err := plistTemplate.Execute(f, data); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing plist: %v\n", err)
		os.Exit(1)
	}

	if err := exec.Command("launchctl", "load", path).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading service: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Installed and started %s\n", plistLabel)
	fmt.Printf("Plist: %s\n", path)
	fmt.Printf("Logs:  %s/serve.log\n", logDir())
}

func uninstallLaunchd() {
	path := plistPath()
	exec.Command("launchctl", "unload", path).Run()

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error removing plist: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Uninstalled %s\n", plistLabel)
}
