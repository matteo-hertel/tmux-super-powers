package duck

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// duckPos represents a single duck's position on the canvas.
type duckPos struct {
	ID int     `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}

// viewerModel is the bubbletea model for the duck viewer TUI.
type viewerModel struct {
	width   int
	height  int
	ducks   []duckPos
	conn    net.Conn
	scanner *bufio.Scanner
	err     error
}

// duckUpdateMsg is sent when the daemon broadcasts new duck positions.
type duckUpdateMsg struct {
	Ducks []duckPos
}

// errMsg is sent when a connection or protocol error occurs.
type errMsg struct {
	err error
}

// subscribeMsg is a JSON command sent to the daemon.
type subscribeMsg struct {
	Cmd    string `json:"cmd"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// connectedMsg is sent once the socket connection is established.
type connectedMsg struct {
	conn net.Conn
}

// RunViewer starts the bubbletea viewer program. Blocking.
// Called when user runs `tsp duck --viewer` (inside tmux popup).
func RunViewer() error {
	p := tea.NewProgram(viewerModel{}, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m viewerModel) Init() tea.Cmd {
	return connectCmd
}

// connectCmd establishes the unix socket connection to the daemon.
func connectCmd() tea.Msg {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		return errMsg{err: err}
	}
	return connectedMsg{conn: conn}
}

func (m viewerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case connectedMsg:
		m.conn = msg.conn
		m.scanner = bufio.NewScanner(m.conn)
		// Send initial subscribe with current dimensions (may be 0 until
		// WindowSizeMsg arrives; the daemon handles 0 as default 80x24).
		sendSubscribe(m.conn, m.width, m.height)
		return m, listenForUpdates(m.scanner)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.conn != nil {
			sendSubscribe(m.conn, m.width, m.height)
		}
		return m, nil

	case duckUpdateMsg:
		m.ducks = msg.Ducks
		return m, listenForUpdates(m.scanner)

	case errMsg:
		m.err = msg.err
		return m, tea.Quit

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			if m.conn != nil {
				sendUnsubscribe(m.conn)
				m.conn.Close()
			}
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m viewerModel) View() string {
	if m.err != nil {
		return "\n  No ducks! Run `tsp duck new` to hatch one.\n"
	}

	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Build a 2D grid of cells. Each cell is one terminal column wide.
	// Initialize with spaces.
	grid := make([][]rune, m.height)
	for y := range grid {
		grid[y] = make([]rune, m.width)
		for x := range grid[y] {
			grid[y][x] = ' '
		}
	}

	// Place each duck on the grid. The duck emoji takes 2 cells wide,
	// so we mark the position with the emoji rune and the next cell
	// with a zero-width placeholder (0) to skip it during rendering.
	const duckEmoji = '\U0001F986'
	for _, d := range m.ducks {
		px := int(d.X)
		py := int(d.Y)
		if py >= 0 && py < m.height && px >= 0 && px+1 < m.width {
			grid[py][px] = duckEmoji
			grid[py][px+1] = 0 // placeholder for second cell of wide emoji
		}
	}

	// Render the grid to a string.
	var b strings.Builder
	for y, row := range grid {
		for _, r := range row {
			if r == 0 {
				// Skip: this cell is the second half of a wide emoji.
				continue
			}
			b.WriteRune(r)
		}
		if y < len(grid)-1 {
			b.WriteByte('\n')
		}
	}

	return b.String()
}

// sendSubscribe sends a subscribe command with the given dimensions.
func sendSubscribe(conn net.Conn, width, height int) {
	msg := subscribeMsg{Cmd: "subscribe", Width: width, Height: height}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	conn.Write(data)
}

// sendUnsubscribe sends an unsubscribe command to the daemon.
func sendUnsubscribe(conn net.Conn) {
	msg := subscribeMsg{Cmd: "unsubscribe"}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	conn.Write(data)
}

// listenForUpdates returns a tea.Cmd that blocks until the next JSON
// message arrives from the daemon, then delivers it as a duckUpdateMsg.
// After each message is processed, Update re-invokes this to wait for
// the next one. The scanner is reused across calls to preserve its
// internal buffer.
func listenForUpdates(scanner *bufio.Scanner) tea.Cmd {
	return func() tea.Msg {
		if scanner.Scan() {
			var update struct {
				Ducks []duckPos `json:"ducks"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &update); err != nil {
				return errMsg{err: fmt.Errorf("invalid message from daemon: %w", err)}
			}
			return duckUpdateMsg{Ducks: update.Ducks}
		}
		if err := scanner.Err(); err != nil {
			return errMsg{err: err}
		}
		// Scanner returned false with no error means EOF (daemon closed connection).
		return errMsg{err: fmt.Errorf("daemon disconnected")}
	}
}
