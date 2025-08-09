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

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Create a new project",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		m := projectModel{
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

		if fm, ok := finalModel.(projectModel); ok && fm.projectName != "" {
			createProject(cfg, fm.projectName)
		}
	},
}

type projectModel struct {
	textInput   textinput.Model
	projectName string
	config      *config.Config
}

func (m projectModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m projectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m projectModel) View() string {
	return fmt.Sprintf(
		"Create a new project\n\n%s\n\n(esc to quit)",
		m.textInput.View(),
	)
}

func createProject(cfg *config.Config, name string) {
	projectsPath := expandPath(cfg.Projects.Path)
	projectPath := filepath.Join(projectsPath, name)

	if err := os.MkdirAll(projectPath, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating project directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Created project: %s\n", projectPath)
	
	sessionName := fmt.Sprintf("project-%s", name)
	
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