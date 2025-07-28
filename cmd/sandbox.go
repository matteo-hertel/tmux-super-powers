package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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

		m := sandboxModel{
			textInput: textinput.New(),
			config:    cfg,
		}
		m.textInput.Placeholder = "Enter project name"
		m.textInput.Focus()
		m.textInput.CharLimit = 50
		m.textInput.Width = 30

		p := tea.NewProgram(m)
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if fm, ok := finalModel.(sandboxModel); ok && fm.projectName != "" {
			createSandboxProject(cfg, fm.projectName)
		}
	},
}

type sandboxModel struct {
	textInput   textinput.Model
	projectName string
	config      *config.Config
}

func (m sandboxModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m sandboxModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m sandboxModel) View() string {
	return fmt.Sprintf(
		"Create a new sandbox project\n\n%s\n\n(esc to quit)",
		m.textInput.View(),
	)
}

func createSandboxProject(cfg *config.Config, name string) {
	sandboxPath := expandPath(cfg.Sandbox.Path)
	projectPath := filepath.Join(sandboxPath, name)

	if err := os.MkdirAll(projectPath, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating project directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Created sandbox project: %s\n", projectPath)
	
	sessionName := fmt.Sprintf("sandbox-%s", name)
	
	checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
	if checkCmd.Run() != nil {
		createSession(sessionName, projectPath)
	}
	
	if os.Getenv("TMUX") != "" {
		cmd := exec.Command("tmux", "switch-client", "-t", sessionName)
		cmd.Run()
	} else {
		cmd := exec.Command("tmux", "attach-session", "-t", sessionName)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	}
}