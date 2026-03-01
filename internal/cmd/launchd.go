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
	return writePlist()
}

// writePlist always writes (or overwrites) the launchd plist file.
// Returns the plist path and any error.
func writePlist() (string, error) {
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

	path := plistPath()
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("creating plist: %w", err)
	}
	defer f.Close()

	if err := plistTemplate.Execute(f, data); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("writing plist: %w", err)
	}

	return path, nil
}

// removePlist removes the launchd plist file.
func removePlist() error {
	path := plistPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing plist: %w", err)
	}
	return nil
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
