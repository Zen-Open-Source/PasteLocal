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

	// fieldStyle for subtle labels inside boxes (calm, readable).
	fieldStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA"))

	// mutedStyle for secondary / placeholder text.
	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))
)

// renderBox renders the given inner content inside a copy of the provided
// box style (which may have Width set for consistent sizing). When termWidth
// > 0 the resulting block is horizontally centered using PlaceHorizontal.
// Three lightweight strategies exist in View:
//   - header: capped Width + Align, then optional Place for full-term centering
//   - boxes: adaptive safe boxW (accounting for border+pad) + Place via this helper
//   - footer: raw Place
//
// This keeps the helper tiny while avoiding overflow on narrow terminals and
// m.width==0 (pre-WindowSizeMsg) initial renders.
func renderBox(inner string, termWidth int, st lipgloss.Style) string {
	s := st.Render(inner)
	if termWidth > 0 {
		s = lipgloss.PlaceHorizontal(termWidth, lipgloss.Center, s)
	}
	return s
}

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
	// Relay v1.0 live fields (parsed from /version, rendered instead of placeholder).
	relayEnabled     bool
	relayURL         string
	relayDeviceID    string
	relayFingerprint string
	relayPeerCount   int
	relayLastPush    string
	relayHealthy     bool
	// Vision v2 status (static note + doctor recommended; live would require /version extension).
	visionStatus string
	// Recall v2 live status (parsed from /version now that we extended the proto).
	recallEnabled bool
	recallDim     int
	recallStatus  string
}

type hostStatus struct {
	Alias   string
	Status  string
	Termius bool
}

// NewModel creates a new dashboard model.
func NewModel(cfg *config.Config, cfgPath string) Model {
	return Model{
		cfg:          cfg,
		cfgPath:      cfgPath,
		port:         cfg.Port,
		visionStatus: "see [vision] in config + `pastelocal doctor` (proactive v2)",
		recallStatus: "disabled — set [recall] + embed command for --search magic",
		// relay* default to zero/false (placeholder path until daemon populates)
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

	// Full-width colored header banner (professional anchor, uses captured width).
	// Capped at 100 for very wide terminals; always PlaceHorizontal-centered when
	// m.width > 0 so it matches the geometry of the content cards and footer.
	title := "pastelocal dashboard"
	titleSt := titleStyle.Copy()
	termW := m.width
	if termW > 0 {
		if termW > 100 {
			termW = 100
		}
		titleSt = titleSt.Width(termW).Align(lipgloss.Center)
	}
	titleRendered := titleSt.Render(title)
	if m.width > 0 {
		titleRendered = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, titleRendered)
	}
	b.WriteString(titleRendered)
	b.WriteString("\n\n")

	// Consistent box width + centering for visual weight and breathing room.
	// targetContentWidth (72) chosen for comfortable reading on typical terminals
	// while degrading gracefully. The calculation ensures final rendered outer
	// width (content + 4 pad + 2 border) never exceeds m.width, preventing
	// overflow/clipping on narrow terminals or the initial m.width==0 render.
	boxW := 60 // safe default when m.width==0 (before first WindowSizeMsg)
	if m.width > 0 {
		// lipgloss bordered+padded box outer width ≈ boxW + 6
		maxOuter := m.width
		desired := 72
		boxW = desired
		if boxW > maxOuter-6 {
			boxW = maxOuter - 6
		}
		if boxW < 20 {
			boxW = 20
		}
	}
	boxSt := boxStyle.Copy().Width(boxW)

	// Daemon status box (status icon + color for instant scannability).
	var statusStyled string
	if m.running {
		if m.healthy {
			statusStyled = statusOkStyle.Render("● running")
		} else {
			statusStyled = statusWarnStyle.Render("◐ degraded")
		}
	} else {
		statusStyled = statusErrStyle.Render("○ stopped")
	}

	daemonLines := []string{
		fmt.Sprintf("%s %s", fieldStyle.Render("Status:"), statusStyled),
		fmt.Sprintf("%s   %d (loopback)", fieldStyle.Render("Port:"), m.port),
	}
	if m.pid > 0 {
		daemonLines = append(daemonLines, fmt.Sprintf("%s    %d", fieldStyle.Render("PID:"), m.pid))
	}
	if m.uptime != "" {
		daemonLines = append(daemonLines, fmt.Sprintf("%s  %s", fieldStyle.Render("Uptime:"), m.uptime))
	}
	daemonInner := strings.Join(daemonLines, "\n")
	b.WriteString(renderBox(daemonInner, m.width, boxSt))
	b.WriteString("\n")

	// Last Read box (graceful placeholders for currently unpopulated fields).
	lastReadStr := "(never)"
	if m.lastRead != "" {
		lastReadStr = m.lastRead
	}
	lastReadVal := lastReadStr
	if lastReadStr == "(never)" {
		lastReadVal = mutedStyle.Render("(never)")
	}
	fmtVal := m.lastFmt
	if fmtVal == "" {
		fmtVal = mutedStyle.Render("—")
	}
	lastReadInner := fmt.Sprintf("%s %s\n%s    %s",
		fieldStyle.Render("Last Read:"), lastReadVal,
		fieldStyle.Render("Format:"), fmtVal)
	b.WriteString(renderBox(lastReadInner, m.width, boxSt))
	b.WriteString("\n")

	// Clipboard Watch box (two-line when change timestamp present for density;
	// icons + colors make enabled/disabled state pop at a glance).
	var watchInner string
	watchLabel := fieldStyle.Render("Clipboard Watch:")
	if m.watchEnabled {
		if m.lastClipboardChange != "" {
			watchInner = fmt.Sprintf("%s %s\n%s     %s",
				watchLabel, statusOkStyle.Render("enabled"), fieldStyle.Render("Last change:"), m.lastClipboardChange)
		} else {
			watchInner = fmt.Sprintf("%s %s (detecting OS clipboard changes)",
				watchLabel, statusOkStyle.Render("enabled"))
		}
	} else {
		hint := mutedStyle.Render("Hint: (set [watch] enabled = true in config)")
		watchInner = fmt.Sprintf("%s %s\n%s",
			watchLabel, statusWarnStyle.Render("disabled"), hint)
	}
	b.WriteString(renderBox(watchInner, m.width, boxSt))
	b.WriteString("\n")

	// Relay (v1.0) box — rich live status (replaces prior placeholder at ~257).
	// Reuses exact renderBox + field/status/muted styles + multi-line layout from watch/lastRead boxes.
	var relayInner string
	if m.relayEnabled {
		peerStr := fmt.Sprintf("%d", m.relayPeerCount)
		if m.relayPeerCount == 0 {
			peerStr = mutedStyle.Render("0 (use `pastelocal relay add-peer`)")
		}
		lastStr := mutedStyle.Render("never")
		if m.relayLastPush != "" {
			lastStr = m.relayLastPush
		}
		healthyStr := statusOkStyle.Render("healthy")
		if !m.relayHealthy {
			healthyStr = statusWarnStyle.Render("unhealthy (run relay pair)")
		}
		relayInner = fmt.Sprintf("%s %s\n%s %s\n%s %s  %s %s\n%s %s  %s %s",
			fieldStyle.Render("Relay (v1.0):"), healthyStr,
			fieldStyle.Render("URL:"), mutedStyle.Render(m.relayURL),
			fieldStyle.Render("Device FP:"), m.relayFingerprint,
			fieldStyle.Render("Peers:"), peerStr,
			fieldStyle.Render("Last push:"), lastStr,
		)
	} else {
		hint := mutedStyle.Render("Hint: set [relay] enabled=true + `pastelocal relay pair`")
		relayInner = fmt.Sprintf("%s %s\n%s",
			fieldStyle.Render("Relay (v1.0):"), statusWarnStyle.Render("disabled"), hint)
	}
	b.WriteString(renderBox(relayInner, m.width, boxSt))
	b.WriteString("\n")

	// Vision v2 status box (proactive/cached analysis). Smallest addition; full live stats
	// would require proto /version extension. Points user to doctor for 5+ checks.
	visionInner := fmt.Sprintf("%s %s",
		fieldStyle.Render("Vision (v2):"), mutedStyle.Render(m.visionStatus))
	b.WriteString(renderBox(visionInner, m.width, boxSt))
	b.WriteString("\n")

	// Recall v2 box — semantic search status (now fully live thanks to /version extension).
	var recallInner string
	if m.recallEnabled {
		dimStr := ""
		if m.recallDim > 0 {
			dimStr = fmt.Sprintf(" (%dd)", m.recallDim)
		}
		statusStr := statusOkStyle.Render("enabled" + dimStr)
		if m.recallStatus != "" && !strings.Contains(m.recallStatus, "ready") {
			statusStr = statusWarnStyle.Render(m.recallStatus)
		}
		recallInner = fmt.Sprintf("%s %s\n%s %s",
			fieldStyle.Render("Recall (v2):"), statusStr,
			fieldStyle.Render("Status:"), mutedStyle.Render(m.recallStatus))
	} else {
		hint := mutedStyle.Render("Hint: [recall] enabled=true + embed command (see docs/examples/)")
		recallInner = fmt.Sprintf("%s %s\n%s",
			fieldStyle.Render("Recall (v2):"), statusWarnStyle.Render("disabled"), hint)
	}
	b.WriteString(renderBox(recallInner, m.width, boxSt))
	b.WriteString("\n")

	// Recent History box (Slice 1 foundation ONLY: static placeholder example data for 3 entries.
	// Real data fetch + model state + refresh() integration deferred to Slice 2 per strict scope.
	// Uses *only* existing boxStyle, renderBox, fieldStyle, mutedStyle (no layout/resize changes).
	recentInner := fmt.Sprintf("%s\n%s %s  %s  %s\n%s %s  %s  %s\n%s %s  %s  %s",
		fieldStyle.Render("Recent History:"),
		mutedStyle.Render("1"), "2026-05-24T12:34:56Z", "png ", mutedStyle.Render("  48192 bytes"),
		mutedStyle.Render("2"), "2026-05-24T12:30:11Z", "text", mutedStyle.Render("   2048 bytes"),
		mutedStyle.Render("3"), "2026-05-24T11:59:59Z", "png ", mutedStyle.Render("  12011 bytes"))
	b.WriteString(renderBox(recentInner, m.width, boxSt))
	b.WriteString("\n")

	// Hosts box (symbols for quick ok/unreachable scan; termius noted subtly).
	if len(m.hosts) > 0 {
		var hostLines []string
		hostLines = append(hostLines, fieldStyle.Render("Hosts:"))
		for _, h := range m.hosts {
			statusDisp := statusOkStyle.Render("✓ ok")
			if h.Status != "ok" {
				statusDisp = statusErrStyle.Render("✗ " + h.Status)
			}
			suffix := ""
			if h.Termius {
				suffix = mutedStyle.Render(" (termius)")
			}
			hostLines = append(hostLines, fmt.Sprintf("  %s  %s%s", h.Alias, statusDisp, suffix))
		}
		b.WriteString(renderBox(strings.Join(hostLines, "\n"), m.width, boxSt))
		b.WriteString("\n")
	} else {
		hostsInner := fmt.Sprintf("%s %s",
			fieldStyle.Render("Hosts:"), mutedStyle.Render("(none configured)"))
		b.WriteString(renderBox(hostsInner, m.width, boxSt))
		b.WriteString("\n")
	}

	// Centered footer with highlighted key (calm, scannable).
	footer := dimStyle.Render("[") + keyStyle.Render("q") + dimStyle.Render("] quit")
	if m.width > 0 {
		footer = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, footer)
	}
	b.WriteString("\n")
	b.WriteString(footer)
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
		// Rebuild hosts from config so the polished Hosts box shows trustworthy
		// "✗ unreachable" instead of stale prior "ok" entries (pre-existing gap
		// now visible due to always-rendered substantial cards).
		m.hosts = m.hosts[:0]
		for alias, h := range m.cfg.Hosts {
			m.hosts = append(m.hosts, hostStatus{
				Alias:   alias,
				Status:  "unreachable",
				Termius: h.Termius,
			})
		}
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
			// Parse relay v1.0 status (exact interface{} pattern as watch fields).
			if r, ok := verData["relay"].(map[string]interface{}); ok {
				if e, ok := r["enabled"].(bool); ok {
					m.relayEnabled = e
				}
				if u, ok := r["relay_url"].(string); ok {
					m.relayURL = u
				}
				if d, ok := r["device_id"].(string); ok {
					m.relayDeviceID = d
				}
				if f, ok := r["fingerprint"].(string); ok {
					m.relayFingerprint = f
				}
				if p, ok := r["peer_count"].(float64); ok { // JSON numbers decode as float64
					m.relayPeerCount = int(p)
				}
				if l, ok := r["last_push"].(string); ok {
					m.relayLastPush = l
				}
				if h, ok := r["healthy"].(bool); ok {
					m.relayHealthy = h
				}
			}
			// Recall v2 (flat scalars from our VersionResponse extension)
			if e, ok := verData["recall_enabled"].(bool); ok {
				m.recallEnabled = e
			}
			if d, ok := verData["recall_dim"].(float64); ok {
				m.recallDim = int(d)
			}
			if st, ok := verData["recall_status"].(string); ok {
				m.recallStatus = st
			}
		}
	}

	// Try to read the last clipboard state from the daemon.
	// LastRead data is intentionally left unpopulated (no /stats endpoint yet);
	// View renders graceful placeholders per polish requirements and non-goals.
}
