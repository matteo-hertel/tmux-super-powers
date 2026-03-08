package duck

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// duck represents a single duck on the canvas.
type duck struct {
	ID             int     `json:"id"`
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
	DX             float64 `json:"-"`
	DY             float64 `json:"-"`
	TicksUntilTurn int     `json:"-"`
}

// command is a client-to-daemon message.
type command struct {
	Cmd    string `json:"cmd"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// response is a daemon-to-client reply for add/remove commands.
type response struct {
	OK    bool `json:"ok"`
	Count int  `json:"count"`
}

// frame is a daemon-to-subscriber broadcast.
type frame struct {
	Ducks []duck `json:"ducks"`
}

// daemon holds the state for the duck daemon process.
type daemon struct {
	mu          sync.Mutex
	ducks       []duck
	nextID      int
	subscribers []net.Conn
	width       int
	height      int
}

const (
	tickInterval = 200 * time.Millisecond
	duckWidth    = 2
)

// tspDir returns the path to ~/.tsp, creating it if needed.
func tspDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".tsp")
	os.MkdirAll(dir, 0700)
	return dir
}

// SocketPath returns the path to the unix socket (~/.tsp/duck.sock).
func SocketPath() string {
	return filepath.Join(tspDir(), "duck.sock")
}

// pidPath returns the path to the PID file (~/.tsp/duck.pid).
func pidPath() string {
	return filepath.Join(tspDir(), "duck.pid")
}

// StartDaemon runs the daemon in the current process (blocking).
// Called by the forked daemon process.
func StartDaemon() error {
	sockPath := SocketPath()

	// Clean up stale socket file.
	os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", sockPath, err)
	}

	// Write PID file.
	pid := os.Getpid()
	pidFile := pidPath()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)+"\n"), 0600); err != nil {
		ln.Close()
		return fmt.Errorf("write pid file: %w", err)
	}

	d := &daemon{
		width:  80,
		height: 24,
	}

	// Clean up on exit.
	cleanup := func() {
		ln.Close()
		os.Remove(sockPath)
		os.Remove(pidFile)
	}

	// Handle signals for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	stopCh := make(chan struct{})

	go func() {
		<-sigCh
		close(stopCh)
		cleanup()
		os.Exit(0)
	}()

	// Start the tick loop.
	go d.tickLoop(stopCh)

	// Accept connections.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-stopCh:
					return
				default:
					continue
				}
			}
			go d.handleConn(conn)
		}
	}()

	// Block until stopped.
	<-stopCh
	cleanup()
	return nil
}

// tickLoop updates duck positions and broadcasts to subscribers every tick.
func (d *daemon) tickLoop(stopCh <-chan struct{}) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			d.tick()
		}
	}
}

// tick updates all duck positions and broadcasts the new state.
func (d *daemon) tick() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i := range d.ducks {
		dk := &d.ducks[i]
		dk.TicksUntilTurn--
		if dk.TicksUntilTurn <= 0 {
			dk.DX, dk.DY = randomVelocity()
			dk.TicksUntilTurn = 5 + rand.IntN(11) // 5-15
		}

		dk.X += dk.DX
		dk.Y += dk.DY

		// Bounce off edges.
		maxX := float64(d.width - duckWidth)
		maxY := float64(d.height - 1)

		if dk.X < 0 {
			dk.X = -dk.X
			dk.DX = math.Abs(dk.DX)
		} else if dk.X > maxX {
			dk.X = maxX - (dk.X - maxX)
			dk.DX = -math.Abs(dk.DX)
		}

		if dk.Y < 0 {
			dk.Y = -dk.Y
			dk.DY = math.Abs(dk.DY)
		} else if dk.Y > maxY {
			dk.Y = maxY - (dk.Y - maxY)
			dk.DY = -math.Abs(dk.DY)
		}
	}

	d.broadcast()
}

// broadcast sends the current duck state to all subscribers.
// Caller must hold d.mu.
func (d *daemon) broadcast() {
	if len(d.subscribers) == 0 {
		return
	}

	f := frame{Ducks: make([]duck, len(d.ducks))}
	copy(f.Ducks, d.ducks)

	data, err := json.Marshal(f)
	if err != nil {
		return
	}
	data = append(data, '\n')

	alive := d.subscribers[:0]
	for _, conn := range d.subscribers {
		conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		if _, err := conn.Write(data); err != nil {
			conn.Close()
			continue
		}
		alive = append(alive, conn)
	}
	d.subscribers = alive
}

// handleConn processes commands from a single client connection.
func (d *daemon) handleConn(conn net.Conn) {
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Bytes()
		var cmd command
		if err := json.Unmarshal(line, &cmd); err != nil {
			continue
		}

		switch cmd.Cmd {
		case "add":
			d.handleAdd(conn)
		case "remove":
			d.handleRemove(conn)
		case "subscribe":
			d.handleSubscribe(conn, cmd.Width, cmd.Height)
			// After subscribing, the conn stays open for broadcasts.
			// Don't close it when the scanner loop ends normally.
			return
		case "unsubscribe":
			d.handleUnsubscribe(conn)
		}
	}

	// Client disconnected — remove from subscribers if present.
	d.mu.Lock()
	d.removeSubscriberLocked(conn)
	d.mu.Unlock()
	conn.Close()
}

// handleAdd creates a new duck at a random position.
func (d *daemon) handleAdd(conn net.Conn) {
	d.mu.Lock()
	d.nextID++
	dx, dy := randomVelocity()
	dk := duck{
		ID:             d.nextID,
		X:              rand.Float64() * float64(d.width-duckWidth),
		Y:              rand.Float64() * float64(d.height-1),
		DX:             dx,
		DY:             dy,
		TicksUntilTurn: 5 + rand.IntN(11),
	}
	d.ducks = append(d.ducks, dk)
	count := len(d.ducks)
	d.mu.Unlock()

	resp, _ := json.Marshal(response{OK: true, Count: count})
	resp = append(resp, '\n')
	conn.Write(resp)
}

// handleRemove removes the highest-ID duck (LIFO).
func (d *daemon) handleRemove(conn net.Conn) {
	d.mu.Lock()
	count := len(d.ducks)
	if count > 0 {
		d.ducks = d.ducks[:count-1]
		count--
	}
	shouldExit := count == 0 && len(d.subscribers) == 0
	d.mu.Unlock()

	resp, _ := json.Marshal(response{OK: true, Count: count})
	resp = append(resp, '\n')
	conn.Write(resp)

	if shouldExit {
		// No ducks and no subscribers — shut down the daemon.
		p, _ := os.FindProcess(os.Getpid())
		p.Signal(syscall.SIGTERM)
	}
}

// handleSubscribe adds the connection to the subscriber list and sends the current state.
func (d *daemon) handleSubscribe(conn net.Conn, width, height int) {
	d.mu.Lock()
	if width > 0 {
		d.width = width
	}
	if height > 0 {
		d.height = height
	}
	d.subscribers = append(d.subscribers, conn)

	// Send current state immediately.
	f := frame{Ducks: make([]duck, len(d.ducks))}
	copy(f.Ducks, d.ducks)
	d.mu.Unlock()

	data, _ := json.Marshal(f)
	data = append(data, '\n')
	conn.Write(data)
}

// handleUnsubscribe removes the connection from the subscriber list.
func (d *daemon) handleUnsubscribe(conn net.Conn) {
	d.mu.Lock()
	d.removeSubscriberLocked(conn)
	d.mu.Unlock()
}

// removeSubscriberLocked removes conn from the subscriber list.
// Caller must hold d.mu.
func (d *daemon) removeSubscriberLocked(conn net.Conn) {
	for i, sub := range d.subscribers {
		if sub == conn {
			d.subscribers = append(d.subscribers[:i], d.subscribers[i+1:]...)
			return
		}
	}
}

// randomVelocity returns a random velocity vector with speed between 1 and 2 cells per tick.
func randomVelocity() (float64, float64) {
	angle := rand.Float64() * 2 * math.Pi
	speed := 1.0 + rand.Float64() // 1-2
	return math.Cos(angle) * speed, math.Sin(angle) * speed
}

// isRunning checks if the daemon is already running by reading the PID file
// and checking if the process is alive.
func isRunning() bool {
	data, err := os.ReadFile(pidPath())
	if err != nil {
		return false
	}

	pid, err := strconv.Atoi(string(data[:len(data)-1])) // trim trailing newline
	if err != nil {
		return false
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds. Send signal 0 to check if alive.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}

	return true
}

// EnsureDaemon checks if the daemon is running. If not, fork-execs the current
// binary with "duck --daemon" to start it. Returns when the daemon is ready.
func EnsureDaemon() error {
	if isRunning() {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	cmd := exec.Command(exe, "duck", "--daemon")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// Detach — don't wait for the child.
	cmd.Process.Release()

	// Wait for the socket to appear (up to 2 seconds).
	sockPath := SocketPath()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	return fmt.Errorf("daemon did not start within 2 seconds")
}

// SendCommand sends a one-shot command to the daemon via socket.
// Returns the response line (if any).
func SendCommand(cmd string) (string, error) {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		return "", fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))

	msg, _ := json.Marshal(command{Cmd: cmd})
	msg = append(msg, '\n')
	if _, err := conn.Write(msg); err != nil {
		return "", fmt.Errorf("send command: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	return "", nil
}
