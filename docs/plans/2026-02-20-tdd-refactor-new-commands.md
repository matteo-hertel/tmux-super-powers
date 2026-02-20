# TDD Refactor + Middle & Peek Commands Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add test coverage to existing code, fix critical bugs, deduplicate shared logic, then add `tsp middle` and `tsp peek` commands.

**Architecture:** Bottom-up — extract pure/testable utilities into `internal/tmux` and `internal/pathutil` packages, write tests first, refactor commands to use them, then build new commands on the clean foundation.

**Tech Stack:** Go 1.24, cobra, bubbletea/bubbles/lipgloss, tmux 3.3+ (display-popup)

**CI safety:** All tests use `t.TempDir()`, `t.Setenv()`, and mock data. No real tmux, git, or home directory access.

---

## Task 1: Create `internal/pathutil` package with tests

**Files:**
- Create: `internal/pathutil/pathutil.go`
- Create: `internal/pathutil/pathutil_test.go`

**Step 1: Write the failing tests**

Create `internal/pathutil/pathutil_test.go`:

```go
package pathutil

import (
	"testing"
)

func TestExpandPath_TildePrefix(t *testing.T) {
	t.Setenv("HOME", "/fake/home")
	got := ExpandPath("~/projects")
	want := "/fake/home/projects"
	if got != want {
		t.Errorf("ExpandPath(\"~/projects\") = %q, want %q", got, want)
	}
}

func TestExpandPath_TildeOnly(t *testing.T) {
	t.Setenv("HOME", "/fake/home")
	got := ExpandPath("~/")
	want := "/fake/home"
	if got != want {
		t.Errorf("ExpandPath(\"~/\") = %q, want %q", got, want)
	}
}

func TestExpandPath_EmptyString(t *testing.T) {
	got := ExpandPath("")
	if got != "" {
		t.Errorf("ExpandPath(\"\") = %q, want \"\"", got)
	}
}

func TestExpandPath_SingleChar(t *testing.T) {
	got := ExpandPath("/")
	if got != "/" {
		t.Errorf("ExpandPath(\"/\") = %q, want \"/\"", got)
	}
}

func TestExpandPath_AbsolutePath(t *testing.T) {
	got := ExpandPath("/usr/local/bin")
	want := "/usr/local/bin"
	if got != want {
		t.Errorf("ExpandPath(\"/usr/local/bin\") = %q, want %q", got, want)
	}
}

func TestExpandEnvVar_Set(t *testing.T) {
	t.Setenv("EDITOR", "nvim")
	got := ExpandEnvVar("$EDITOR")
	if got != "nvim" {
		t.Errorf("ExpandEnvVar(\"$EDITOR\") = %q, want \"nvim\"", got)
	}
}

func TestExpandEnvVar_Unset(t *testing.T) {
	t.Setenv("EDITOR", "")
	got := ExpandEnvVar("$EDITOR")
	if got != "" {
		t.Errorf("ExpandEnvVar(\"$EDITOR\") = %q, want \"\"", got)
	}
}

func TestExpandEnvVar_LiteralString(t *testing.T) {
	got := ExpandEnvVar("vim")
	if got != "vim" {
		t.Errorf("ExpandEnvVar(\"vim\") = %q, want \"vim\"", got)
	}
}

func TestExpandEnvVar_EmptyString(t *testing.T) {
	got := ExpandEnvVar("")
	if got != "" {
		t.Errorf("ExpandEnvVar(\"\") = %q, want \"\"", got)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/pathutil/ -v`
Expected: compilation error — package doesn't exist yet

**Step 3: Write minimal implementation**

Create `internal/pathutil/pathutil.go`:

```go
package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandPath expands ~ prefix to the user's home directory.
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// ExpandEnvVar expands a $VAR string to its environment variable value.
// Returns the string unchanged if it doesn't start with $.
func ExpandEnvVar(s string) string {
	if strings.HasPrefix(s, "$") {
		return os.Getenv(s[1:])
	}
	return s
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/pathutil/ -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/pathutil/
git commit -m "feat: add pathutil package with ExpandPath and ExpandEnvVar

Fixes expandPath panic on empty/short strings by using strings.HasPrefix.
Adds ExpandEnvVar for $VARIABLE expansion in config values."
```

---

## Task 2: Create `internal/tmux` package with tests

**Files:**
- Create: `internal/tmux/tmux.go`
- Create: `internal/tmux/tmux_test.go`

**Step 1: Write the failing tests**

Create `internal/tmux/tmux_test.go`:

```go
package tmux

import (
	"testing"
)

func TestSanitizeSessionName_Dots(t *testing.T) {
	got := SanitizeSessionName("my.project")
	want := "my-project"
	if got != want {
		t.Errorf("SanitizeSessionName(\"my.project\") = %q, want %q", got, want)
	}
}

func TestSanitizeSessionName_Colons(t *testing.T) {
	got := SanitizeSessionName("foo:bar")
	want := "foo-bar"
	if got != want {
		t.Errorf("SanitizeSessionName(\"foo:bar\") = %q, want %q", got, want)
	}
}

func TestSanitizeSessionName_Multiple(t *testing.T) {
	got := SanitizeSessionName("my.project:v2.0")
	want := "my-project-v2-0"
	if got != want {
		t.Errorf("SanitizeSessionName(\"my.project:v2.0\") = %q, want %q", got, want)
	}
}

func TestSanitizeSessionName_Clean(t *testing.T) {
	got := SanitizeSessionName("my-project")
	want := "my-project"
	if got != want {
		t.Errorf("SanitizeSessionName(\"my-project\") = %q, want %q", got, want)
	}
}

func TestSanitizeSessionName_Empty(t *testing.T) {
	got := SanitizeSessionName("")
	want := ""
	if got != want {
		t.Errorf("SanitizeSessionName(\"\") = %q, want %q", got, want)
	}
}

func TestBuildSessionArgs_NewSession(t *testing.T) {
	args := BuildNewSessionArgs("test-session", "/tmp/dir", "nvim")
	expected := []string{"new-session", "-d", "-s", "test-session", "-c", "/tmp/dir", "nvim"}
	if len(args) != len(expected) {
		t.Fatalf("BuildNewSessionArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestBuildSessionArgs_NoCommand(t *testing.T) {
	args := BuildNewSessionArgs("test-session", "/tmp/dir", "")
	expected := []string{"new-session", "-d", "-s", "test-session", "-c", "/tmp/dir"}
	if len(args) != len(expected) {
		t.Fatalf("BuildNewSessionArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestBuildPopupArgs_DefaultSize(t *testing.T) {
	args := BuildPopupArgs("htop", 75, 75)
	expected := []string{"display-popup", "-E", "-w", "75%", "-h", "75%", "htop"}
	if len(args) != len(expected) {
		t.Fatalf("BuildPopupArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestBuildPopupArgs_CustomSize(t *testing.T) {
	args := BuildPopupArgs("lazydocker", 90, 60)
	expected := []string{"display-popup", "-E", "-w", "90%", "-h", "60%", "lazydocker"}
	if len(args) != len(expected) {
		t.Fatalf("BuildPopupArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestIsInsideTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-501/default,12345,0")
	if !IsInsideTmux() {
		t.Error("IsInsideTmux() = false, want true")
	}
}

func TestIsInsideTmux_Outside(t *testing.T) {
	t.Setenv("TMUX", "")
	if IsInsideTmux() {
		t.Error("IsInsideTmux() = true, want false")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tmux/ -v`
Expected: compilation error — package doesn't exist yet

**Step 3: Write minimal implementation**

Create `internal/tmux/tmux.go`:

```go
package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SanitizeSessionName replaces tmux-problematic characters (. and :) with hyphens.
func SanitizeSessionName(name string) string {
	r := strings.NewReplacer(".", "-", ":", "-")
	return r.Replace(name)
}

// IsInsideTmux returns true if running inside a tmux session.
func IsInsideTmux() bool {
	return os.Getenv("TMUX") != ""
}

// SessionExists checks if a tmux session with the given name exists.
func SessionExists(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// KillSession kills a tmux session by name.
func KillSession(name string) error {
	return exec.Command("tmux", "kill-session", "-t", name).Run()
}

// AttachOrSwitch attaches to or switches to a tmux session.
// Uses switch-client when inside tmux, attach-session when outside.
func AttachOrSwitch(name string) error {
	if IsInsideTmux() {
		return exec.Command("tmux", "switch-client", "-t", name).Run()
	}
	cmd := exec.Command("tmux", "attach-session", "-t", name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// BuildNewSessionArgs builds the tmux args for creating a new session.
// Uses -c flag for working directory (no shell injection).
func BuildNewSessionArgs(name, dir, command string) []string {
	args := []string{"new-session", "-d", "-s", name, "-c", dir}
	if command != "" {
		args = append(args, command)
	}
	return args
}

// BuildPopupArgs builds the tmux args for display-popup.
func BuildPopupArgs(command string, width, height int) []string {
	return []string{
		"display-popup", "-E",
		"-w", fmt.Sprintf("%d%%", width),
		"-h", fmt.Sprintf("%d%%", height),
		command,
	}
}

// RunPopup runs a command in a tmux display-popup overlay.
func RunPopup(command string, width, height int) error {
	args := BuildPopupArgs(command, width, height)
	cmd := exec.Command("tmux", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CreateTwoPaneSession creates a tmux session with a left and right pane.
// Uses -c flag for directory — no shell injection via send-keys.
func CreateTwoPaneSession(name, dir, leftCmd, rightCmd string) error {
	args := BuildNewSessionArgs(name, dir, leftCmd)
	if err := exec.Command("tmux", args...).Run(); err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	splitArgs := []string{"split-window", "-h", "-t", name, "-c", dir}
	if rightCmd != "" {
		splitArgs = append(splitArgs, rightCmd)
	}
	if err := exec.Command("tmux", splitArgs...).Run(); err != nil {
		return fmt.Errorf("failed to split window: %w", err)
	}

	exec.Command("tmux", "select-pane", "-t", name+":0.0").Run()
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/tmux/ -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/tmux/
git commit -m "feat: add tmux package with session helpers and arg builders

Extracts tmux interaction into a testable package. Includes SanitizeSessionName
to fix session name issues with . and : characters. Uses -c flag for directories
to prevent shell injection."
```

---

## Task 3: Add config tests and fix config.go

**Files:**
- Create: `config/config_test.go`
- Modify: `config/config.go`

**Step 1: Write the failing tests**

Create `config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	cfg, err := LoadFrom(filepath.Join(tmpDir, ".tmux-super-powers.yaml"))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadFrom() returned nil config")
	}
	if len(cfg.Directories) == 0 {
		t.Error("expected default directories, got empty")
	}
}

func TestLoad_ValidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := []byte("directories:\n  - /tmp/projects\neditor: nano\n")
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if len(cfg.Directories) != 1 || cfg.Directories[0] != "/tmp/projects" {
		t.Errorf("Directories = %v, want [/tmp/projects]", cfg.Directories)
	}
	if cfg.Editor != "nano" {
		t.Errorf("Editor = %q, want \"nano\"", cfg.Editor)
	}
}

func TestLoad_EditorEnvExpansion(t *testing.T) {
	t.Setenv("EDITOR", "nvim")
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := []byte("editor: $EDITOR\n")
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.Editor != "nvim" {
		t.Errorf("Editor = %q, want \"nvim\"", cfg.Editor)
	}
}

func TestLoad_EditorFallback(t *testing.T) {
	t.Setenv("EDITOR", "")
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := []byte("directories:\n  - /tmp\n")
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.Editor != "vim" {
		t.Errorf("Editor = %q, want \"vim\"", cfg.Editor)
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	original := &Config{
		Directories: []string{"/tmp/a", "/tmp/b"},
		Editor:      "code",
		Sandbox:     Sandbox{Path: "/tmp/sandbox"},
		Projects:    Projects{Path: "/tmp/projects"},
	}

	if err := SaveTo(original, configPath); err != nil {
		t.Fatalf("SaveTo() error = %v", err)
	}

	loaded, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if loaded.Editor != original.Editor {
		t.Errorf("Editor = %q, want %q", loaded.Editor, original.Editor)
	}
	if len(loaded.Directories) != len(original.Directories) {
		t.Errorf("Directories length = %d, want %d", len(loaded.Directories), len(original.Directories))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./config/ -v`
Expected: compilation error — `LoadFrom` and `SaveTo` don't exist yet

**Step 3: Modify config.go to add LoadFrom/SaveTo and fix issues**

Replace `config/config.go` with:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Directories       []string `yaml:"directories"`
	IgnoreDirectories []string `yaml:"ignore_directories"`
	Sandbox           Sandbox  `yaml:"sandbox"`
	Projects          Projects `yaml:"projects"`
	Editor            string   `yaml:"editor"`
}

type Sandbox struct {
	Path string `yaml:"path"`
}

type Projects struct {
	Path string `yaml:"path"`
}

// Load loads config from the default path (~/.tmux-super-powers.yaml).
func Load() (*Config, error) {
	return LoadFrom(ConfigPath())
}

// LoadFrom loads config from a specific file path.
func LoadFrom(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Expand $VAR style editor values
	if strings.HasPrefix(cfg.Editor, "$") {
		cfg.Editor = os.Getenv(cfg.Editor[1:])
	}

	if cfg.Editor == "" {
		cfg.Editor = os.Getenv("EDITOR")
		if cfg.Editor == "" {
			cfg.Editor = "vim"
		}
	}

	return &cfg, nil
}

// Save saves config to the default path.
func Save(cfg *Config) error {
	return SaveTo(cfg, ConfigPath())
}

// SaveTo saves config to a specific file path.
func SaveTo(cfg *Config, configPath string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

func defaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	return &Config{
		Directories: []string{
			filepath.Join(homeDir, "projects"),
			filepath.Join(homeDir, "work"),
		},
		Sandbox: Sandbox{
			Path: filepath.Join(homeDir, "sandbox"),
		},
		Projects: Projects{
			Path: filepath.Join(homeDir, "projects"),
		},
		Editor: os.Getenv("EDITOR"),
	}
}

// ConfigPath returns the default config file path.
func ConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".tmux-super-powers.yaml")
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./config/ -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add config/
git commit -m "feat: add testable LoadFrom/SaveTo, fix env var expansion and HOME usage

Adds LoadFrom/SaveTo for testability. Expands \$EDITOR in config values.
Replaces os.Getenv(\"HOME\") with os.UserHomeDir() throughout."
```

---

## Task 4: Extract helpers and add tests

**Files:**
- Create: `internal/cmd/helpers_test.go`
- Modify: `internal/cmd/gtmux.go` (move `detectPackageManager` out — but keep it here since it's in this package)
- Modify: `internal/cmd/dir.go` (keep `buildIgnoreSet`/`shouldIgnoreDir` here too)

Since all these functions are in `package cmd` already and are used by multiple files in the same package, we keep them where they are but add tests.

Create `internal/cmd/helpers_test.go`:

**Step 1: Write the failing tests**

```go
package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectPackageManager_BunLockb(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bun.lockb"), []byte{}, 0644)
	got := detectPackageManager(dir)
	if got != "bun" {
		t.Errorf("detectPackageManager() = %q, want \"bun\"", got)
	}
}

func TestDetectPackageManager_BunLock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bun.lock"), []byte{}, 0644)
	got := detectPackageManager(dir)
	if got != "bun" {
		t.Errorf("detectPackageManager() = %q, want \"bun\"", got)
	}
}

func TestDetectPackageManager_Pnpm(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte{}, 0644)
	got := detectPackageManager(dir)
	if got != "pnpm" {
		t.Errorf("detectPackageManager() = %q, want \"pnpm\"", got)
	}
}

func TestDetectPackageManager_Yarn(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "yarn.lock"), []byte{}, 0644)
	got := detectPackageManager(dir)
	if got != "yarn" {
		t.Errorf("detectPackageManager() = %q, want \"yarn\"", got)
	}
}

func TestDetectPackageManager_Npm(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte{}, 0644)
	got := detectPackageManager(dir)
	if got != "npm" {
		t.Errorf("detectPackageManager() = %q, want \"npm\"", got)
	}
}

func TestDetectPackageManager_PackageJsonFallback(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644)
	got := detectPackageManager(dir)
	if got != "npm" {
		t.Errorf("detectPackageManager() = %q, want \"npm\"", got)
	}
}

func TestDetectPackageManager_None(t *testing.T) {
	dir := t.TempDir()
	got := detectPackageManager(dir)
	if got != "" {
		t.Errorf("detectPackageManager() = %q, want \"\"", got)
	}
}

func TestDetectPackageManager_Priority(t *testing.T) {
	dir := t.TempDir()
	// bun should win over yarn
	os.WriteFile(filepath.Join(dir, "bun.lockb"), []byte{}, 0644)
	os.WriteFile(filepath.Join(dir, "yarn.lock"), []byte{}, 0644)
	got := detectPackageManager(dir)
	if got != "bun" {
		t.Errorf("detectPackageManager() = %q, want \"bun\" (priority)", got)
	}
}

func TestShouldIgnoreDir_Hidden(t *testing.T) {
	if !shouldIgnoreDir(".git", map[string]bool{}) {
		t.Error("shouldIgnoreDir(\".git\") = false, want true")
	}
}

func TestShouldIgnoreDir_UserIgnore(t *testing.T) {
	ignores := map[string]bool{"node_modules": true}
	if !shouldIgnoreDir("node_modules", ignores) {
		t.Error("shouldIgnoreDir(\"node_modules\") = false, want true")
	}
}

func TestShouldIgnoreDir_CaseInsensitive(t *testing.T) {
	ignores := map[string]bool{"node_modules": true}
	if !shouldIgnoreDir("Node_Modules", ignores) {
		t.Error("shouldIgnoreDir(\"Node_Modules\") with lowercase key = false, want true")
	}
}

func TestShouldIgnoreDir_Normal(t *testing.T) {
	if shouldIgnoreDir("src", map[string]bool{}) {
		t.Error("shouldIgnoreDir(\"src\") = true, want false")
	}
}

func TestBuildIgnoreSet_Empty(t *testing.T) {
	set := buildIgnoreSet(nil)
	if len(set) != 0 {
		t.Errorf("buildIgnoreSet(nil) length = %d, want 0", len(set))
	}
}

func TestBuildIgnoreSet_WithEntries(t *testing.T) {
	set := buildIgnoreSet([]string{"node_modules", "vendor"})
	if !set["node_modules"] {
		t.Error("expected node_modules in set")
	}
	if !set["vendor"] {
		t.Error("expected vendor in set")
	}
}

func TestParseWorktreesPorcelain(t *testing.T) {
	// Simulates git worktree list --porcelain output
	// First entry is always the main worktree — should be skipped
	input := `worktree /Users/my-user/my-repo
branch refs/heads/main

worktree /Users/my-user/work/my-repo-feature-one
branch refs/heads/feature-one

worktree /Users/my-user/work/my-repo-fix-bug
branch refs/heads/fix-bug
`
	worktrees := parseWorktreesPorcelain(input)

	if len(worktrees) != 2 {
		t.Fatalf("parseWorktreesPorcelain() returned %d worktrees, want 2", len(worktrees))
	}
	if worktrees[0].Branch != "feature-one" {
		t.Errorf("worktrees[0].Branch = %q, want \"feature-one\"", worktrees[0].Branch)
	}
	if worktrees[0].Path != "/Users/my-user/work/my-repo-feature-one" {
		t.Errorf("worktrees[0].Path = %q, want \"/Users/my-user/work/my-repo-feature-one\"", worktrees[0].Path)
	}
	if worktrees[1].Branch != "fix-bug" {
		t.Errorf("worktrees[1].Branch = %q, want \"fix-bug\"", worktrees[1].Branch)
	}
}

func TestParseWorktreesPorcelain_HyphenInMainPath(t *testing.T) {
	// The main worktree path contains hyphens — must still be skipped
	input := `worktree /Users/my-user/my-cool-repo
branch refs/heads/main

worktree /tmp/my-cool-repo-dev
branch refs/heads/dev
`
	worktrees := parseWorktreesPorcelain(input)

	if len(worktrees) != 1 {
		t.Fatalf("parseWorktreesPorcelain() returned %d worktrees, want 1", len(worktrees))
	}
	if worktrees[0].Branch != "dev" {
		t.Errorf("worktrees[0].Branch = %q, want \"dev\"", worktrees[0].Branch)
	}
}

func TestParseWorktreesPorcelain_Empty(t *testing.T) {
	worktrees := parseWorktreesPorcelain("")
	if len(worktrees) != 0 {
		t.Errorf("parseWorktreesPorcelain(\"\") returned %d, want 0", len(worktrees))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmd/ -run "TestDetect|TestShould|TestBuild|TestParse" -v`
Expected: compilation error — `parseWorktreesPorcelain` doesn't exist yet

**Step 3: Extract `parseWorktreesPorcelain` from `getWorktrees`**

Add to `internal/cmd/gtwremove.go` (after the `getWorktrees` function, around line 191):

```go
// parseWorktreesPorcelain parses the output of `git worktree list --porcelain`.
// Skips the first entry (main worktree) by index.
func parseWorktreesPorcelain(output string) []Worktree {
	if output == "" {
		return nil
	}

	var worktrees []Worktree
	lines := strings.Split(strings.TrimSpace(output), "\n")

	var current Worktree
	isFirst := true
	entryIndex := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if entryIndex > 0 && current.Path != "" && current.Branch != "" {
				worktrees = append(worktrees, current)
			}
			current = Worktree{}
			entryIndex++
			isFirst = false
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current.Path = strings.TrimPrefix(line, "worktree ")
			if isFirst {
				// Mark that we've seen the first worktree line
				isFirst = false
			}
		} else if strings.HasPrefix(line, "branch ") {
			branch := strings.TrimPrefix(line, "branch ")
			branch = strings.TrimPrefix(branch, "refs/heads/")
			current.Branch = branch
		}
	}

	// Handle last entry (no trailing blank line)
	if entryIndex > 0 && current.Path != "" && current.Branch != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees
}
```

Then update `getWorktrees()` to use it:

```go
func getWorktrees() ([]Worktree, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseWorktreesPorcelain(string(output)), nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmd/ -run "TestDetect|TestShould|TestBuild|TestParse" -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/cmd/helpers_test.go internal/cmd/gtwremove.go
git commit -m "test: add tests for detectPackageManager, shouldIgnoreDir, parseWorktreesPorcelain

Extracts parseWorktreesPorcelain for testability. Fixes flawed main worktree
detection that relied on hyphens in path — now skips first entry by index."
```

---

## Task 5: Fix shell injection in createSession and wire up tmux package

**Files:**
- Modify: `internal/cmd/dir.go:447-457` (replace `createSession`)
- Modify: `internal/cmd/dir.go:436-444` (replace inline attach logic)
- Modify: `internal/cmd/sandbox.go:97-113` (replace `createSession` + inline attach)
- Modify: `internal/cmd/project.go:97-113` (replace `createSession` + inline attach)
- Modify: `internal/cmd/list.go:117-128` (replace `attachToSession`)
- Modify: `internal/cmd/gtmux.go:190-198` (replace `createGitWorktreeSession`)
- Modify: `internal/cmd/gtmuxhere.go:43` (use new function)
- Modify: `internal/cmd/gtwremove.go` (use `tmux.KillSession`, `tmux.SessionExists`)

**Step 1: Replace `createSession` in dir.go**

Replace `createSession` (dir.go lines 447-457) with:

```go
func createSession(sessionName, dir string) {
	tmuxpkg.CreateTwoPaneSession(sessionName, dir, "nvim", "")
}
```

Add import `tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"` to dir.go.

Replace the inline attach block in `openSelectedDirs` (dir.go lines 436-444) with:

```go
	sessionName := tmuxpkg.SanitizeSessionName(filepath.Base(paths[0]))
	tmuxpkg.AttachOrSwitch(sessionName)
```

Also sanitize the session name where it's created (dir.go line 427):

```go
	sessionName := tmuxpkg.SanitizeSessionName(filepath.Base(path))
```

**Step 2: Replace inline attach in sandbox.go and project.go**

In `sandbox.go` replace lines 97-113:

```go
	sessionName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("sandbox-%s", name))

	if !tmuxpkg.SessionExists(sessionName) {
		tmuxpkg.CreateTwoPaneSession(sessionName, projectPath, "nvim", "")
	}

	tmuxpkg.AttachOrSwitch(sessionName)
```

Same pattern for `project.go` lines 97-113 (using `"project-%s"` prefix).

Add import `tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"` to both.

**Step 3: Replace `attachToSession` in list.go**

Replace `attachToSession` (list.go lines 117-128) with:

```go
func attachToSession(session string) {
	tmuxpkg.AttachOrSwitch(session)
}
```

Add import `tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"`.

**Step 4: Replace `createGitWorktreeSession` in gtmux.go**

Replace `createGitWorktreeSession` (gtmux.go lines 190-198) with:

```go
func createGitWorktreeSession(sessionName, path string) {
	tmuxpkg.KillSession(sessionName)
	tmuxpkg.CreateTwoPaneSession(sessionName, path, "nvim", "claude --dangerously-skip-permissions")
}
```

Sanitize the session name at creation site (gtmux.go line 80):

```go
	sessionName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", repoName, branch))
```

Same for gtmuxhere.go line 38:

```go
	sessionName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", repoName, currentBranch))
```

Add import `tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"` to both.

**Step 5: Wire up tmux package in gtwremove.go**

Replace `tmuxSessionExists` calls with `tmuxpkg.SessionExists`, `exec.Command("tmux", "kill-session"...)` with `tmuxpkg.KillSession`. Sanitize session name:

```go
sessionName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", repoName, wt.Branch))
```

Add import. Remove the now-unused `tmuxSessionExists` function.

**Step 6: Replace `expandPath` in dir.go with pathutil**

Replace `expandPath` function in dir.go (lines 234-240) with a call to the pathutil package:

```go
import pathutil "github.com/matteo-hertel/tmux-super-powers/internal/pathutil"
```

Then find-replace all calls to `expandPath(` with `pathutil.ExpandPath(` in dir.go, sandbox.go, and project.go. Delete the old `expandPath` function from dir.go.

**Step 7: Verify everything compiles and tests pass**

Run: `go build ./... && go test ./... -v`
Expected: all PASS, zero compilation errors

**Step 8: Verify no shell injection remains**

Run: `grep -rn '"cd "+' internal/`
Expected: zero matches

**Step 9: Commit**

```bash
git add internal/ config/
git commit -m "refactor: wire up tmux and pathutil packages, fix shell injection

Replaces all inline tmux logic with tmux package calls.
Fixes shell injection in createSession by using -c flag.
Sanitizes all session names. Centralizes AttachOrSwitch.
Replaces expandPath with pathutil.ExpandPath."
```

---

## Task 6: Fix wtx-rm parallelization (git lock race)

**Files:**
- Modify: `internal/cmd/gtwremove.go:193-262`

**Step 1: Restructure removeSelectedWorktrees**

Replace `removeSelectedWorktrees` with a two-phase approach:

```go
func removeSelectedWorktrees(worktrees []Worktree, selected map[int]bool) {
	repoName := getRepoName()

	// Collect selected worktrees
	type selectedWorktree struct {
		idx int
		wt  Worktree
	}
	var toRemove []selectedWorktree
	for idx := range selected {
		if idx < len(worktrees) {
			toRemove = append(toRemove, selectedWorktree{idx: idx, wt: worktrees[idx]})
		}
	}

	// Phase 1: Parallel — kill tmux sessions and remove directories
	var wg sync.WaitGroup
	results := make([]string, len(worktrees))

	for _, sw := range toRemove {
		wg.Add(1)
		go func(idx int, wt Worktree) {
			defer wg.Done()
			sessionName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", repoName, wt.Branch))
			var out strings.Builder

			out.WriteString(fmt.Sprintf("Removing worktree: %s (%s)\n", wt.Branch, wt.Path))

			if tmuxpkg.SessionExists(sessionName) {
				out.WriteString(fmt.Sprintf("  Killing tmux session '%s'...\n", sessionName))
				tmuxpkg.KillSession(sessionName)
			}

			if _, err := os.Stat(wt.Path); err == nil {
				out.WriteString(fmt.Sprintf("  Removing directory '%s'...\n", wt.Path))
				if err := os.RemoveAll(wt.Path); err != nil {
					out.WriteString(fmt.Sprintf("  Warning: Failed to remove directory: %v\n", err))
				} else {
					out.WriteString("  Directory removed successfully.\n")
				}
			}

			cleanupEmptyParentsCollect(wt.Path, &out)
			results[idx] = out.String()
		}(sw.idx, sw.wt)
	}

	wg.Wait()

	// Print parallel phase results
	for _, r := range results {
		if r != "" {
			fmt.Print(r)
		}
	}

	// Phase 2: Sequential — git operations (avoids lock contention)
	for _, sw := range toRemove {
		wt := sw.wt

		cmd := exec.Command("git", "worktree", "remove", wt.Path, "--force")
		if err := cmd.Run(); err != nil {
			fmt.Printf("  Warning: git worktree remove failed for %s: %v\n", wt.Branch, err)
		} else {
			fmt.Printf("  Worktree reference for '%s' removed.\n", wt.Branch)
		}

		cmd = exec.Command("git", "branch", "-D", wt.Branch)
		if err := cmd.Run(); err != nil {
			fmt.Printf("  Warning: Failed to delete branch '%s': %v\n", wt.Branch, err)
		} else {
			fmt.Printf("  Branch '%s' deleted.\n", wt.Branch)
		}
	}

	fmt.Println("Worktree removal completed.")
}
```

**Step 2: Remove dead code**

Delete the old `cleanupEmptyParents` function (lines 287-316) and the `tmuxSessionExists` function (lines 271-274) — both now replaced by the tmux package.

**Step 3: Verify build and tests**

Run: `go build ./... && go test ./... -v`
Expected: all PASS

**Step 4: Commit**

```bash
git add internal/cmd/gtwremove.go
git commit -m "fix: resolve git lock race in parallel worktree removal

Splits removal into two phases: parallel (tmux kill + directory removal)
and sequential (git worktree remove + branch delete). Removes dead code."
```

---

## Task 7: Deduplicate sandbox.go and project.go

**Files:**
- Create: `internal/cmd/project_creator.go`
- Modify: `internal/cmd/sandbox.go`
- Modify: `internal/cmd/project.go`

**Step 1: Create shared project creator**

Create `internal/cmd/project_creator.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	pathutil "github.com/matteo-hertel/tmux-super-powers/internal/pathutil"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
)

type projectCreatorConfig struct {
	Title         string
	Placeholder   string
	BasePath      string
	SessionPrefix string
}

type creatorModel struct {
	textInput   textinput.Model
	projectName string
	title       string
}

func (m creatorModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m creatorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			value := strings.TrimSpace(m.textInput.Value())
			if value != "" {
				m.projectName = value
				return m, tea.Quit
			}
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m creatorModel) View() string {
	return fmt.Sprintf(
		"%s\n\n%s\n\n(esc to quit)",
		m.title,
		m.textInput.View(),
	)
}

func runProjectCreator(cfg projectCreatorConfig) {
	ti := textinput.New()
	ti.Placeholder = cfg.Placeholder
	ti.Focus()
	ti.CharLimit = 50
	ti.Width = 30

	m := creatorModel{
		textInput: ti,
		title:     cfg.Title,
	}

	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fm, ok := finalModel.(creatorModel)
	if !ok || fm.projectName == "" {
		return
	}

	basePath := pathutil.ExpandPath(cfg.BasePath)
	projectPath := filepath.Join(basePath, fm.projectName)

	if err := os.MkdirAll(projectPath, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Created: %s\n", projectPath)

	sessionName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", cfg.SessionPrefix, fm.projectName))

	if !tmuxpkg.SessionExists(sessionName) {
		tmuxpkg.CreateTwoPaneSession(sessionName, projectPath, "nvim", "")
	}

	tmuxpkg.AttachOrSwitch(sessionName)
}
```

**Step 2: Slim down sandbox.go**

Replace `internal/cmd/sandbox.go` with:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/spf13/cobra"
)

var sandboxCmd = &cobra.Command{
	Use:   "sandbox",
	Short: "Create a new sandbox project",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		runProjectCreator(projectCreatorConfig{
			Title:         "Create a new sandbox project",
			Placeholder:   "Enter project name",
			BasePath:      cfg.Sandbox.Path,
			SessionPrefix: "sandbox",
		})
	},
}
```

**Step 3: Slim down project.go**

Replace `internal/cmd/project.go` with:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Create a new project",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		runProjectCreator(projectCreatorConfig{
			Title:         "Create a new project",
			Placeholder:   "Enter project name",
			BasePath:      cfg.Projects.Path,
			SessionPrefix: "project",
		})
	},
}
```

**Step 4: Verify build and tests**

Run: `go build ./... && go test ./... -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/cmd/project_creator.go internal/cmd/sandbox.go internal/cmd/project.go
git commit -m "refactor: deduplicate sandbox and project commands into shared creator"
```

---

## Task 8: Fix doc drift — rename txl to list

**Files:**
- Modify: `internal/cmd/list.go:16`
- Modify: `CLAUDE.md`

**Step 1: Rename the command**

In `list.go` line 16, change:
```go
Use:   "txl",
```
to:
```go
Use:   "list",
Aliases: []string{"txl"},
```

**Step 2: Update CLAUDE.md**

Update the default behavior description to accurately state that `tsp` (with no subcommand) shows help, and that `tsp list` (or `tsp txl`) is the session management command.

**Step 3: Verify**

Run: `go build ./... && go test ./... -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/cmd/list.go CLAUDE.md
git commit -m "fix: rename txl command to list, keep txl as alias, update docs"
```

---

## Task 9: Add `tsp middle` command

**Files:**
- Create: `internal/cmd/middle.go`
- Create: `internal/cmd/middle_test.go`
- Modify: `internal/cmd/root.go` (register command)

**Step 1: Write the failing tests**

Create `internal/cmd/middle_test.go`:

```go
package cmd

import (
	"testing"
)

func TestMiddleBuildArgs_DefaultSize(t *testing.T) {
	args := buildMiddleArgs("htop", 75, 75)
	expected := []string{"display-popup", "-E", "-w", "75%", "-h", "75%", "htop"}
	if len(args) != len(expected) {
		t.Fatalf("buildMiddleArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestMiddleBuildArgs_CustomSize(t *testing.T) {
	args := buildMiddleArgs("lazydocker", 90, 60)
	expected := []string{"display-popup", "-E", "-w", "90%", "-h", "60%", "lazydocker"}
	if len(args) != len(expected) {
		t.Fatalf("buildMiddleArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestMiddleResolveSize_DefaultsOnly(t *testing.T) {
	w, h := resolveMiddleSize(75, 0, 0)
	if w != 75 || h != 75 {
		t.Errorf("resolveMiddleSize(75, 0, 0) = (%d, %d), want (75, 75)", w, h)
	}
}

func TestMiddleResolveSize_WidthOverride(t *testing.T) {
	w, h := resolveMiddleSize(75, 90, 0)
	if w != 90 || h != 75 {
		t.Errorf("resolveMiddleSize(75, 90, 0) = (%d, %d), want (90, 75)", w, h)
	}
}

func TestMiddleResolveSize_HeightOverride(t *testing.T) {
	w, h := resolveMiddleSize(75, 0, 60)
	if w != 75 || h != 60 {
		t.Errorf("resolveMiddleSize(75, 0, 60) = (%d, %d), want (75, 60)", w, h)
	}
}

func TestMiddleResolveSize_BothOverride(t *testing.T) {
	w, h := resolveMiddleSize(75, 90, 60)
	if w != 90 || h != 60 {
		t.Errorf("resolveMiddleSize(75, 90, 60) = (%d, %d), want (90, 60)", w, h)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmd/ -run "TestMiddle" -v`
Expected: compilation error — functions don't exist

**Step 3: Write the implementation**

Create `internal/cmd/middle.go`:

```go
package cmd

import (
	"fmt"
	"os"

	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var middleCmd = &cobra.Command{
	Use:   "middle [command]",
	Short: "Run a command in a centered popup overlay",
	Long: `Opens a centered tmux popup overlay and runs the specified command.
The popup disappears when the command exits.

Requires tmux 3.3+ and must be run inside a tmux session.

Examples:
  tsp middle htop
  tsp middle lazydocker
  tsp middle "npm run test" --size 90
  tsp middle claude --width 85 --height 70`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if !tmuxpkg.IsInsideTmux() {
			fmt.Fprintf(os.Stderr, "Error: must be run inside a tmux session\n")
			os.Exit(1)
		}

		size, _ := cmd.Flags().GetInt("size")
		width, _ := cmd.Flags().GetInt("width")
		height, _ := cmd.Flags().GetInt("height")

		w, h := resolveMiddleSize(size, width, height)

		if err := tmuxpkg.RunPopup(args[0], w, h); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	middleCmd.Flags().Int("size", 75, "Popup size as percentage of terminal (width and height)")
	middleCmd.Flags().Int("width", 0, "Override popup width percentage (0 = use --size)")
	middleCmd.Flags().Int("height", 0, "Override popup height percentage (0 = use --size)")
}

func resolveMiddleSize(size, width, height int) (int, int) {
	w := size
	h := size
	if width > 0 {
		w = width
	}
	if height > 0 {
		h = height
	}
	return w, h
}

func buildMiddleArgs(command string, width, height int) []string {
	return tmuxpkg.BuildPopupArgs(command, width, height)
}
```

**Step 4: Register the command in root.go**

Add to `internal/cmd/root.go` init():
```go
rootCmd.AddCommand(middleCmd)
```

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/cmd/ -run "TestMiddle" -v`
Expected: all PASS

**Step 6: Build and verify**

Run: `go build ./... && go test ./... -v`
Expected: all PASS

**Step 7: Commit**

```bash
git add internal/cmd/middle.go internal/cmd/middle_test.go internal/cmd/root.go
git commit -m "feat: add tsp middle command — centered popup overlay

Uses tmux display-popup for a floating overlay that disappears on exit.
Supports --size, --width, --height flags for customization."
```

---

## Task 10: Add `tsp peek` command

**Files:**
- Create: `internal/cmd/peek.go`
- Create: `internal/cmd/peek_test.go`
- Modify: `internal/cmd/root.go` (register command)

**Step 1: Write the failing tests**

Create `internal/cmd/peek_test.go`:

```go
package cmd

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPeekModel_QuitOnQ(t *testing.T) {
	m := peekModel{
		sessions: []string{"session1", "session2"},
		cursor:   0,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit command, got nil")
	}
}

func TestPeekModel_QuitOnEsc(t *testing.T) {
	m := peekModel{
		sessions: []string{"session1"},
		cursor:   0,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit command, got nil")
	}
}

func TestPeekModel_NavigateDown(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1", "s2", "s3"},
		cursor:   0,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyDown}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.cursor != 1 {
		t.Errorf("cursor = %d, want 1", pm.cursor)
	}
}

func TestPeekModel_NavigateUp(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1", "s2", "s3"},
		cursor:   2,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyUp}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.cursor != 1 {
		t.Errorf("cursor = %d, want 1", pm.cursor)
	}
}

func TestPeekModel_NavigateDownWrap(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1", "s2"},
		cursor:   1,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyDown}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (wrapped)", pm.cursor)
	}
}

func TestPeekModel_NavigateUpWrap(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1", "s2"},
		cursor:   0,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyUp}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (wrapped)", pm.cursor)
	}
}

func TestPeekModel_EnterSelectsSession(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1", "s2"},
		cursor:   1,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.selected != "s2" {
		t.Errorf("selected = %q, want \"s2\"", pm.selected)
	}
}

func TestPeekModel_TabCyclesPane(t *testing.T) {
	m := peekModel{
		sessions:    []string{"s1"},
		cursor:      0,
		previewPane: 0,
		width:       120,
		height:      40,
	}

	msg := tea.KeyMsg{Type: tea.KeyTab}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.previewPane != 1 {
		t.Errorf("previewPane = %d, want 1", pm.previewPane)
	}
}

func TestPeekModel_WindowSizeMsg(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1"},
	}

	msg := tea.WindowSizeMsg{Width: 200, Height: 50}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.width != 200 || pm.height != 50 {
		t.Errorf("size = (%d, %d), want (200, 50)", pm.width, pm.height)
	}
}

func TestPeekModel_ViewRenders(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1", "s2"},
		cursor:   0,
		preview:  "some content",
		width:    120,
		height:   40,
	}

	view := m.View()
	if view == "" {
		t.Error("View() returned empty string")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmd/ -run "TestPeek" -v`
Expected: compilation error — `peekModel` doesn't exist

**Step 3: Write the implementation**

Create `internal/cmd/peek.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var peekCmd = &cobra.Command{
	Use:   "peek [session]",
	Short: "Live preview of tmux sessions",
	Long: `Interactive dashboard showing all tmux sessions with live pane preview.

Without arguments, opens an interactive TUI:
- Arrow keys to navigate sessions
- Tab to cycle panes within the previewed session
- Enter to jump to a session
- q/Esc to quit

With a session name, prints a one-shot capture and exits.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 1 {
			// Direct mode: capture and print
			content := capturePaneContent(args[0], 0)
			fmt.Print(content)
			return
		}

		if !tmuxpkg.IsInsideTmux() {
			fmt.Fprintf(os.Stderr, "Error: interactive peek must be run inside a tmux session\n")
			os.Exit(1)
		}

		sessions, err := getTmuxSessions()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting sessions: %v\n", err)
			os.Exit(1)
		}

		if len(sessions) == 0 {
			fmt.Println("No tmux sessions found")
			return
		}

		m := peekModel{
			sessions: sessions,
			preview:  capturePaneContent(sessions[0], 0),
		}

		p := tea.NewProgram(m, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if fm, ok := finalModel.(peekModel); ok && fm.selected != "" {
			tmuxpkg.AttachOrSwitch(fm.selected)
		}
	},
}

type tickMsg time.Time

type peekModel struct {
	sessions    []string
	cursor      int
	selected    string
	preview     string
	previewPane int
	width       int
	height      int
}

func (m peekModel) Init() tea.Cmd {
	return tickCmd()
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m peekModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		if len(m.sessions) > 0 {
			m.preview = capturePaneContent(m.sessions[m.cursor], m.previewPane)
		}
		return m, tickCmd()

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(m.sessions) - 1
			}
			m.previewPane = 0
			m.preview = capturePaneContent(m.sessions[m.cursor], m.previewPane)
			return m, nil
		case tea.KeyDown:
			if m.cursor < len(m.sessions)-1 {
				m.cursor++
			} else {
				m.cursor = 0
			}
			m.previewPane = 0
			m.preview = capturePaneContent(m.sessions[m.cursor], m.previewPane)
			return m, nil
		case tea.KeyEnter:
			if len(m.sessions) > 0 {
				m.selected = m.sessions[m.cursor]
			}
			return m, tea.Quit
		case tea.KeyTab:
			m.previewPane++
			m.preview = capturePaneContent(m.sessions[m.cursor], m.previewPane)
			return m, nil
		default:
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "j":
				if m.cursor < len(m.sessions)-1 {
					m.cursor++
				} else {
					m.cursor = 0
				}
				m.previewPane = 0
				m.preview = capturePaneContent(m.sessions[m.cursor], m.previewPane)
				return m, nil
			case "k":
				if m.cursor > 0 {
					m.cursor--
				} else {
					m.cursor = len(m.sessions) - 1
				}
				m.previewPane = 0
				m.preview = capturePaneContent(m.sessions[m.cursor], m.previewPane)
				return m, nil
			}
		}
	}

	return m, nil
}

func (m peekModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	leftWidth := m.width * 30 / 100
	rightWidth := m.width - leftWidth - 3 // 3 for border/margin

	// Left panel: session list
	var sessionLines []string
	for i, s := range m.sessions {
		if i == m.cursor {
			style := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("212")).
				Background(lipgloss.Color("236"))
			sessionLines = append(sessionLines, style.Render(fmt.Sprintf(" > %s ", s)))
		} else {
			sessionLines = append(sessionLines, fmt.Sprintf("   %s", s))
		}
	}

	leftPanel := lipgloss.NewStyle().
		Width(leftWidth).
		Height(m.height - 4).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1).
		Render(strings.Join(sessionLines, "\n"))

	// Right panel: pane preview
	previewContent := m.preview
	if previewContent == "" {
		previewContent = "No content"
	}

	// Truncate preview lines to fit
	lines := strings.Split(previewContent, "\n")
	maxLines := m.height - 6
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	previewContent = strings.Join(lines, "\n")

	paneLabel := fmt.Sprintf(" pane %d ", m.previewPane)
	rightPanel := lipgloss.NewStyle().
		Width(rightWidth).
		Height(m.height - 4).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Render(previewContent)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		Render("  Peek — arrows/jk: navigate | tab: cycle panes | enter: jump | q: quit" + paneLabel)

	layout := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	return fmt.Sprintf("%s\n%s", title, layout)
}

func capturePaneContent(session string, pane int) string {
	target := fmt.Sprintf("%s:0.%d", session, pane)
	cmd := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-e")
	output, err := cmd.Output()
	if err != nil {
		// If pane doesn't exist, reset to pane 0
		if pane > 0 {
			return capturePaneContent(session, 0)
		}
		return fmt.Sprintf("(unable to capture: %v)", err)
	}
	return string(output)
}
```

**Step 4: Register the command in root.go**

Add to `internal/cmd/root.go` init():
```go
rootCmd.AddCommand(peekCmd)
```

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/cmd/ -run "TestPeek" -v`
Expected: all PASS

**Step 6: Build and verify**

Run: `go build ./... && go test ./... -v`
Expected: all PASS

**Step 7: Commit**

```bash
git add internal/cmd/peek.go internal/cmd/peek_test.go internal/cmd/root.go
git commit -m "feat: add tsp peek command — live tmux session dashboard

Two-panel TUI with session list and live pane preview. Refreshes every
500ms via tmux capture-pane. Tab cycles panes, enter jumps to session."
```

---

## Task 11: Final verification and install

**Step 1: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: all PASS across all packages

**Step 2: Build and install**

Run: `go build -o tsp ./cmd/tsp && go install ./cmd/tsp`

**Step 3: Smoke test**

Run (inside tmux):
- `tsp middle htop` — verify popup appears centered, closes on exit
- `tsp peek` — verify two-panel TUI, navigation works
- `tsp list` — verify renamed command works
- `tsp txl` — verify alias still works

**Step 4: Final commit (if any cleanup needed)**

```bash
git add -A
git commit -m "chore: final cleanup after TDD refactor and new commands"
```
