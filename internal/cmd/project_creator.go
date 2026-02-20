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
