package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pikostack/pikostack/internal/config"
	"github.com/pikostack/pikostack/internal/db"
	"github.com/pikostack/pikostack/internal/monitor"
)

type view int

const (
	viewDashboard view = iota
	viewServices
	viewDeploy
	viewLogs
	viewHelp
)

// tickMsg fires on interval to refresh data
type tickMsg time.Time

// dataMsg carries refreshed service list
type dataMsg struct {
	services []db.Service
	summary  *db.ServiceSummary
	stats    map[string]interface{}
}

type actionResultMsg struct {
	err error
	msg string
}

type Model struct {
	db      *db.Database
	mon     *monitor.Monitor
	cfg     *config.Config
	width   int
	height  int
	current view
	spinner spinner.Model

	// Dashboard
	services []db.Service
	summary  *db.ServiceSummary
	stats    map[string]interface{}

	// Services table
	table    table.Model
	selected int

	// Deploy form
	deployInputs  []textinput.Model
	deployFocus   int
	deployType    string
	deployMsg     string

	// Status bar
	statusMsg  string
	statusTime time.Time

	loading bool
}

func Run(database *db.Database, mon *monitor.Monitor, cfg *config.Config) error {
	m := newModel(database, mon, cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newModel(database *db.Database, mon *monitor.Monitor, cfg *config.Config) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED"))

	// Build deploy form inputs
	inputs := make([]textinput.Model, 5)
	placeholders := []string{"Service name", "Type (docker/process/systemd/url)", "Image / Command / Unit / URL", "Ports (8080:80)", "Project ID (optional)"}
	for i := range inputs {
		t := textinput.New()
		t.Placeholder = placeholders[i]
		t.CharLimit = 200
		inputs[i] = t
	}
	inputs[0].Focus()

	cols := []table.Column{
		{Title: "Name", Width: 20},
		{Title: "Type", Width: 10},
		{Title: "Status", Width: 10},
		{Title: "Restarts", Width: 8},
		{Title: "Health", Width: 12},
		{Title: "Project", Width: 16},
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(15),
	)
	t.SetStyles(tableStyles())

	return &Model{
		db:           database,
		mon:          mon,
		cfg:          cfg,
		current:      viewDashboard,
		spinner:      sp,
		table:        t,
		deployInputs: inputs,
		loading:      true,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.fetchData(),
		tickEvery(10*time.Second),
	)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.current != viewDeploy {
				return m, tea.Quit
			}
		case "1":
			m.current = viewDashboard
		case "2":
			m.current = viewServices
		case "3":
			m.current = viewDeploy
		case "4":
			m.current = viewLogs
		case "?":
			m.current = viewHelp
		case "r":
			if m.current == viewServices && len(m.services) > 0 {
				idx := m.table.Cursor()
				if idx < len(m.services) {
					svc := &m.services[idx]
					return m, m.restartService(svc)
				}
			}
		case "s":
			if m.current == viewServices && len(m.services) > 0 {
				idx := m.table.Cursor()
				if idx < len(m.services) {
					svc := &m.services[idx]
					return m, m.stopService(svc)
				}
			}
		case "enter":
			if m.current == viewDeploy {
				return m, m.submitDeploy()
			}
		case "tab":
			if m.current == viewDeploy {
				m.deployInputs[m.deployFocus].Blur()
				m.deployFocus = (m.deployFocus + 1) % len(m.deployInputs)
				m.deployInputs[m.deployFocus].Focus()
			}
		case "shift+tab":
			if m.current == viewDeploy {
				m.deployInputs[m.deployFocus].Blur()
				m.deployFocus = (m.deployFocus - 1 + len(m.deployInputs)) % len(m.deployInputs)
				m.deployInputs[m.deployFocus].Focus()
			}
		case "esc":
			if m.current == viewDeploy {
				m.current = viewDashboard
			}
		}

	case tickMsg:
		cmds = append(cmds, m.fetchData(), tickEvery(10*time.Second))

	case dataMsg:
		m.loading = false
		m.services = msg.services
		m.summary = msg.summary
		m.stats = msg.stats
		m.rebuildTable()

	case actionResultMsg:
		if msg.err != nil {
			m.statusMsg = "✕ " + msg.err.Error()
		} else {
			m.statusMsg = "✓ " + msg.msg
		}
		m.statusTime = time.Now()
		cmds = append(cmds, m.fetchData())

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	if m.current == viewServices {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		cmds = append(cmds, cmd)
	}

	if m.current == viewDeploy {
		for i := range m.deployInputs {
			var cmd tea.Cmd
			m.deployInputs[i], cmd = m.deployInputs[i].Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) View() string {
	if m.width == 0 {
		return "initializing..."
	}

	header := m.renderHeader()
	nav := m.renderNav()
	content := m.renderContent()
	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		nav,
		content,
		statusBar,
	)
}

// ─── Rendering ────────────────────────────────────────────────────────────────

func (m *Model) renderHeader() string {
	logo := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7C3AED")).
		Bold(true).
		Render("⬡ PIKOSTACK")

	tagline := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Render(" v0.1.0 — service management")

	sysInfo := ""
	if m.stats != nil {
		cpu, _ := m.stats["cpu"].(string)
		mem, _ := m.stats["mem_pct"].(string)
		sysInfo = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4B5563")).
			Render(fmt.Sprintf("CPU %s%%  MEM %s%%", cpu, mem))
	}

	left := logo + tagline
	right := sysInfo

	pad := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if pad < 0 {
		pad = 0
	}

	header := left + strings.Repeat(" ", pad) + right
	return lipgloss.NewStyle().
		Padding(0, 1).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Width(m.width).
		Render(header)
}

func (m *Model) renderNav() string {
	items := []struct {
		key   string
		label string
		v     view
	}{
		{"1", "Dashboard", viewDashboard},
		{"2", "Services", viewServices},
		{"3", "Deploy", viewDeploy},
		{"4", "Logs", viewLogs},
		{"?", "Help", viewHelp},
	}

	var parts []string
	for _, item := range items {
		style := lipgloss.NewStyle().Padding(0, 2)
		if m.current == item.v {
			style = style.
				Background(lipgloss.Color("#7C3AED")).
				Foreground(lipgloss.Color("#FFFFFF")).
				Bold(true)
		} else {
			style = style.
				Foreground(lipgloss.Color("#9CA3AF"))
		}
		parts = append(parts, style.Render(fmt.Sprintf("[%s] %s", item.key, item.label)))
	}

	nav := strings.Join(parts, " ")
	return lipgloss.NewStyle().
		Padding(0, 1).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Width(m.width).
		Render(nav)
}

func (m *Model) renderContent() string {
	contentHeight := m.height - 8 // header + nav + status
	style := lipgloss.NewStyle().
		Height(contentHeight).
		Width(m.width).
		Padding(1, 2)

	switch m.current {
	case viewDashboard:
		return style.Render(m.renderDashboard())
	case viewServices:
		return style.Render(m.renderServices())
	case viewDeploy:
		return style.Render(m.renderDeploy())
	case viewHelp:
		return style.Render(m.renderHelp())
	default:
		return style.Render("Coming soon...")
	}
}

func (m *Model) renderDashboard() string {
	if m.loading {
		return m.spinner.View() + " Loading..."
	}

	// Stats row
	var statCards []string
	if m.summary != nil {
		statCards = []string{
			statCard("Total", fmt.Sprintf("%d", m.summary.Total), "#7C3AED"),
			statCard("Running", fmt.Sprintf("%d", m.summary.Running), "#10B981"),
			statCard("Stopped", fmt.Sprintf("%d", m.summary.Stopped), "#6B7280"),
			statCard("Error", fmt.Sprintf("%d", m.summary.Error), "#EF4444"),
		}
	}

	statsRow := lipgloss.JoinHorizontal(lipgloss.Top, statCards...)

	// Service list (compact)
	var serviceLines []string
	for _, svc := range m.services {
		dot := statusDot(string(svc.Status))
		line := fmt.Sprintf("  %s  %-20s  %-10s  %-10s",
			dot,
			truncate(svc.Name, 20),
			svc.Type,
			svc.Status,
		)
		serviceLines = append(serviceLines, line)
	}

	if len(serviceLines) == 0 {
		serviceLines = []string{
			lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("  No services registered. Press [3] to deploy."),
		}
	}

	svcSection := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(0, 1).
		Render(
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E5E7EB")).Render("Services\n") +
				strings.Join(serviceLines, "\n"),
		)

	return lipgloss.JoinVertical(lipgloss.Left,
		statsRow,
		"",
		svcSection,
	)
}

func (m *Model) renderServices() string {
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).
		Render("[r] restart  [s] stop  [↑↓] navigate")

	return lipgloss.JoinVertical(lipgloss.Left,
		m.table.View(),
		"",
		hint,
	)
}

func (m *Model) renderDeploy() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C3AED")).Render("Deploy New Service")
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("[Tab] next field  [Enter] deploy  [Esc] cancel")

	labels := []string{"Name", "Type", "Image/Command/URL", "Ports", "Project ID"}

	var fields []string
	for i, input := range m.deployInputs {
		label := lipgloss.NewStyle().
			Width(20).
			Foreground(lipgloss.Color("#9CA3AF")).
			Render(labels[i] + ":")

		inputStyle := lipgloss.NewStyle()
		if i == m.deployFocus {
			inputStyle = inputStyle.Foreground(lipgloss.Color("#7C3AED"))
		}
		fields = append(fields, label+inputStyle.Render(input.View()))
	}

	msg := ""
	if m.deployMsg != "" {
		msg = "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Render("✓ "+m.deployMsg)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		strings.Join(fields, "\n"),
		"",
		hint,
		msg,
	)
}

func (m *Model) renderHelp() string {
	rows := [][]string{
		{"1-4", "Switch views"},
		{"q", "Quit"},
		{"r", "Restart selected service (Services view)"},
		{"s", "Stop selected service (Services view)"},
		{"Tab/Shift+Tab", "Navigate deploy form fields"},
		{"Enter", "Submit deploy form"},
		{"Esc", "Back / cancel"},
		{"↑ ↓", "Navigate table"},
	}

	var lines []string
	for _, r := range rows {
		key := lipgloss.NewStyle().Width(20).Foreground(lipgloss.Color("#7C3AED")).Bold(true).Render(r[0])
		desc := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")).Render(r[1])
		lines = append(lines, key+desc)
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(1, 2).
		Render(
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E5E7EB")).Render("Keyboard Shortcuts\n\n") +
				strings.Join(lines, "\n"),
		)
}

func (m *Model) renderStatusBar() string {
	msg := m.statusMsg
	if time.Since(m.statusTime) > 5*time.Second {
		msg = "Pikostack running  •  Pikoview at http://" + m.cfg.Server.Host + fmt.Sprintf(":%d", m.cfg.Server.Port)
	}
	return lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(lipgloss.Color("#6B7280")).
		Background(lipgloss.Color("#111827")).
		Width(m.width).
		Render(msg)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (m *Model) rebuildTable() {
	var rows []table.Row
	for _, svc := range m.services {
		health := "✓"
		if !svc.LastHealthOK {
			health = "✕"
		}
		project := ""
		if svc.Project != nil {
			project = svc.Project.Name
		}
		rows = append(rows, table.Row{
			svc.Name,
			string(svc.Type),
			string(svc.Status),
			fmt.Sprintf("%d", svc.RestartCount),
			health,
			project,
		})
	}
	m.table.SetRows(rows)
}

func (m *Model) fetchData() tea.Cmd {
	return func() tea.Msg {
		services, _ := m.db.ListServices()
		summary, _ := m.db.GetServiceSummary()
		stats := m.mon.GetSystemStats()
		return dataMsg{services: services, summary: summary, stats: stats}
	}
}

func (m *Model) restartService(svc *db.Service) tea.Cmd {
	return func() tea.Msg {
		err := m.mon.RestartService(svc)
		if err != nil {
			return actionResultMsg{err: err}
		}
		return actionResultMsg{msg: svc.Name + " restarted"}
	}
}

func (m *Model) stopService(svc *db.Service) tea.Cmd {
	return func() tea.Msg {
		err := m.mon.StopService(svc)
		if err != nil {
			return actionResultMsg{err: err}
		}
		return actionResultMsg{msg: svc.Name + " stopped"}
	}
}

func (m *Model) submitDeploy() tea.Cmd {
	return func() tea.Msg {
		name := m.deployInputs[0].Value()
		svcType := m.deployInputs[1].Value()
		target := m.deployInputs[2].Value()
		ports := m.deployInputs[3].Value()
		projectID := m.deployInputs[4].Value()

		if name == "" || svcType == "" || target == "" {
			return actionResultMsg{err: fmt.Errorf("name, type, and target are required")}
		}

		svc := &db.Service{
			Name:      name,
			Type:      db.ServiceType(svcType),
			ProjectID: projectID,
			Ports:     ports,
		}
		switch db.ServiceType(svcType) {
		case db.ServiceTypeDocker:
			svc.Image = target
			svc.ContainerName = name
		case db.ServiceTypeProcess:
			svc.Command = target
		case db.ServiceTypeSystemd:
			svc.SystemdUnit = target
		case db.ServiceTypeURL:
			svc.HealthURL = target
			svc.WatchOnly = true
			svc.AutoRestart = false
		case db.ServiceTypeCompose:
			svc.ComposeFile = target
		}

		if err := m.db.CreateService(svc); err != nil {
			return actionResultMsg{err: err}
		}
		// Reset form
		for i := range m.deployInputs {
			m.deployInputs[i].Reset()
		}
		m.deployInputs[0].Focus()
		m.deployFocus = 0
		return actionResultMsg{msg: "service '" + name + "' deployed"}
	}
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func statCard(label, value, color string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(0, 2).
		MarginRight(1).
		Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Render(label+"\n") +
				lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).Render(value),
		)
}

func statusDot(status string) string {
	colors := map[string]string{
		"running":  "#10B981",
		"error":    "#EF4444",
		"starting": "#F59E0B",
		"stopped":  "#6B7280",
	}
	c, ok := colors[status]
	if !ok {
		c = "#6B7280"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("●")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func tableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("#D1D5DB"))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7C3AED")).
		Bold(false)
	return s
}
