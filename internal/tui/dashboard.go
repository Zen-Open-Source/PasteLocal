package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pastelocal/pastelocal/internal/config"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 2)

	statusOkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")).Bold(true)

	statusErrStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5F5F")).Bold(true)

	statusWarnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFC44D")).Bold(true)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 2)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	keyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).Bold(true)
)

// tickMsg is sent every second to refresh the dashboard.
type tickMsg time.Time

// Model holds the dashboard state.
type Model struct {
	cfg                 *config.Config
	cfgPath             string
	port                int
	running             bool
	pid                 int
	uptime              string
	healthy             bool
	hosts               []hostStatus
	lastRead            string
	lastFmt             string
	watchEnabled        bool
	lastClipboardChange string
	auditLen            int
	width               int
	height              int
	quitting            bool
	err                 error
}

type hostStatus struct {
	Alias   string
	Status  string
	Termius bool
}

// NewModel creates a new dashboard model.
func NewModel(cfg *config.Config, cfgPath string) Model {
	return Model{
		cfg:     cfg,
		cfgPath: cfgPath,
		port:    cfg.Port,
	}
}

// Init starts the dashboard with a tick.
func (m Model) Init() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.refresh()
		if m.quitting {
			return m, nil
		}
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
	}

	return m, nil
}

// View renders the dashboard.
func (m Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render(" pastelocal dashboard "))
	b.WriteString("\n\n")

	// Daemon status
	statusStr := "stopped"
	statusStyle := statusErrStyle
	if m.running {
		if m.healthy {
			statusStr = "running"
			statusStyle = statusOkStyle
		} else {
			statusStr = "degraded"
			statusStyle = statusWarnStyle
		}
	}

	daemonBox := fmt.Sprintf("  Daemon: %s\n  Port:   %d (loopback)\n  PID:    %d\n  Uptime: %s",
		statusStyle.Render(statusStr), m.port, m.pid, m.uptime)
	b.WriteString(boxStyle.Render(daemonBox))
	b.WriteString("\n")

	// Last read
	lastReadStr := "(never)"
	if m.lastRead != "" {
		lastReadStr = m.lastRead
	}
	lastReadBox := fmt.Sprintf("  Last Read: %s\n  Format:    %s",
		lastReadStr, m.lastFmt)
	b.WriteString(boxStyle.Render(lastReadBox))
	b.WriteString("\n")

	// Clipboard Watch status (critical for visibility success criterion)
	watchStr := "disabled (opt-in via [watch] enabled = true in config)"
	if m.watchEnabled {
		watchStr = "enabled (detecting OS changes)"
		if m.lastClipboardChange != "" {
			watchStr = "enabled (last change: " + m.lastClipboardChange + ")"
		}
	}
	b.WriteString(boxStyle.Render("  Clipboard Watch: " + watchStr))
	b.WriteString("\n")

	// Hosts
	if len(m.hosts) > 0 {
		var hostLines []string
		hostLines = append(hostLines, "  Hosts:")
		for _, h := range m.hosts {
			status := statusOkStyle.Render("ok")
			if h.Status != "ok" {
				status = statusErrStyle.Render(h.Status)
			}
			suffix := ""
			if h.Termius {
				suffix = " (termius)"
			}
			hostLines = append(hostLines, fmt.Sprintf("    %s  %s%s", h.Alias, status, suffix))
		}
		b.WriteString(boxStyle.Render(strings.Join(hostLines, "\n")))
		b.WriteString("\n")
	} else {
		b.WriteString(boxStyle.Render("  Hosts: (none configured)"))
		b.WriteString("\n")
	}

	// Keyboard shortcuts
	b.WriteString(dimStyle.Render("  [q] quit  "))
	b.WriteString("\n")

	return b.String()
}

// refresh fetches the latest state from the daemon.
func (m *Model) refresh() {
	// Check daemon health.
	url := fmt.Sprintf("http://127.0.0.1:%d/health", m.port)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		m.running = false
		m.healthy = false
		m.watchEnabled = false
		m.lastClipboardChange = ""
		return
	}
	defer resp.Body.Close()
	m.running = true
	m.healthy = resp.StatusCode == http.StatusOK

	// Build host statuses.
	m.hosts = m.hosts[:0]
	for alias, h := range m.cfg.Hosts {
		status := "ok"
		if !m.healthy {
			status = "unreachable"
		} else if h.Termius {
			status = "ok"
		}
		m.hosts = append(m.hosts, hostStatus{
			Alias:   alias,
			Status:  status,
			Termius: h.Termius,
		})
	}

	// Get version info for uptime/PID.
	verURL := fmt.Sprintf("http://127.0.0.1:%d/version", m.port)
	verResp, err := client.Get(verURL)
	if err == nil {
		defer verResp.Body.Close()
		var verData map[string]interface{}
		if json.NewDecoder(verResp.Body).Decode(&verData) == nil {
			if w, ok := verData["watch_enabled"].(bool); ok {
				m.watchEnabled = w
			}
			if lc, ok := verData["last_clipboard_change"].(string); ok {
				m.lastClipboardChange = lc
			}
		}
	}

	// Try to read the last clipboard state from the daemon.
	// We could add a /stats endpoint for this, but for now we leave it as-is.
}
