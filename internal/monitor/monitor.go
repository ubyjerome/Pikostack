package monitor

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"sync"
	"time"

	"github.com/pikostack/pikostack/internal/config"
	"github.com/pikostack/pikostack/internal/db"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

// EventBroadcast is sent to all WebSocket listeners
type EventBroadcast struct {
	ServiceID string
	Type      string
	Message   string
	Time      time.Time
}

// Monitor drives the background health-check and restart loop
type Monitor struct {
	db          *db.Database
	cfg         *config.Config
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
	subscribers []chan EventBroadcast
	// track running process PIDs for process-type services
	procs    map[string]*exec.Cmd
	reloadCh chan time.Duration
}

func New(database *db.Database, cfg *config.Config) *Monitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Monitor{
		db:     database,
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
		procs:    make(map[string]*exec.Cmd),
		reloadCh: make(chan time.Duration, 1),
	}
}

func (m *Monitor) Start() {
	go m.healthLoop()
	go m.systemMetricsLoop()
	go m.pruneLoop()
	log.Println("monitor: started")
}

func (m *Monitor) Stop() {
	m.cancel()
}

// Subscribe returns a channel that receives all broadcast events.
// The caller must call Unsubscribe when done.
func (m *Monitor) Subscribe() chan EventBroadcast {
	ch := make(chan EventBroadcast, 64)
	m.mu.Lock()
	m.subscribers = append(m.subscribers, ch)
	m.mu.Unlock()
	return ch
}

func (m *Monitor) Unsubscribe(ch chan EventBroadcast) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, sub := range m.subscribers {
		if sub == ch {
			m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

func (m *Monitor) broadcast(ev EventBroadcast) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ch := range m.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
}

// ─── Health Loop ─────────────────────────────────────────────────────────────

func (m *Monitor) healthLoop() {
	m.mu.RLock()
	interval := m.cfg.Monitor.Interval
	m.mu.RUnlock()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case d := <-m.reloadCh:
			ticker.Reset(d)
		case <-ticker.C:
			m.checkAllServices()
		}
	}
}

func (m *Monitor) checkAllServices() {
	services, err := m.db.ListServices()
	if err != nil {
		log.Printf("monitor: list services: %v", err)
		return
	}
	for i := range services {
		go m.checkService(&services[i])
	}
}

func (m *Monitor) checkService(svc *db.Service) {
	ok, status := m.probeService(svc)

	m.db.RecordHealth(svc.ID, ok)

	msg := "OK"
	if !ok {
		msg = fmt.Sprintf("unhealthy: %s", status)
	}
	m.db.RecordEvent(svc.ID, db.EventHealthCheck, msg)

	m.broadcast(EventBroadcast{
		ServiceID: svc.ID,
		Type:      string(db.EventHealthCheck),
		Message:   msg,
		Time:      time.Now(),
	})

	// Only auto-restart if the service was previously running (not just deployed)
	if !ok && !svc.WatchOnly && svc.AutoRestart && svc.Status == db.StatusRunning {
		m.handleUnhealthy(svc)
	}

	// Update status in DB
	newStatus := db.StatusRunning
	if !ok {
		newStatus = db.StatusError
	}
	if newStatus != svc.Status {
		m.db.UpdateServiceStatus(svc.ID, newStatus, 0)
	}
}

func (m *Monitor) probeService(svc *db.Service) (bool, string) {
	switch svc.Type {
	case db.ServiceTypeURL:
		return m.probeURL(svc)
	case db.ServiceTypeDocker:
		return m.probeDocker(svc)
	case db.ServiceTypeCompose:
		return m.probeCompose(svc)
	case db.ServiceTypeProcess:
		return m.probeProcess(svc)
	case db.ServiceTypeSystemd:
		return m.probeSystemd(svc)
	case db.ServiceTypeStatic:
		return m.probeStatic(svc)
	}
	return false, "unknown service type"
}

func (m *Monitor) probeURL(svc *db.Service) (bool, string) {
	if svc.HealthURL == "" {
		return false, "no health URL configured"
	}
	method := svc.HealthMethod
	if method == "" {
		method = "GET"
	}
	req, err := http.NewRequestWithContext(m.ctx, method, svc.HealthURL, nil)
	if err != nil {
		return false, err.Error()
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	expected := svc.HealthStatusCode
	if expected == 0 {
		expected = 200
	}
	if resp.StatusCode != expected {
		return false, fmt.Sprintf("status %d (expected %d)", resp.StatusCode, expected)
	}
	return true, "OK"
}

func (m *Monitor) probeDocker(svc *db.Service) (bool, string) {
	name := svc.ContainerName
	if name == "" {
		name = svc.Name
	}
	out, err := exec.CommandContext(m.ctx, "docker", "inspect", "--format",
		"{{.State.Status}}", name).Output()
	if err != nil {
		return false, "container not found"
	}
	status := strings.TrimSpace(string(out))
	if status == "running" {
		// Also probe health URL if configured
		if svc.HealthURL != "" {
			return m.probeURL(svc)
		}
		return true, "OK"
	}
	return false, "container status: " + status
}

func (m *Monitor) probeCompose(svc *db.Service) (bool, string) {
	cf := svc.ComposeFile
	if cf == "" {
		cf = "docker-compose.yml"
	}
	out, err := exec.CommandContext(m.ctx, "docker", "compose", "-f", cf, "ps", "--format", "json").Output()
	if err != nil {
		return false, err.Error()
	}
	// Simple check: if output contains "running" we consider it healthy
	if strings.Contains(strings.ToLower(string(out)), "running") {
		return true, "OK"
	}
	return false, "no running containers in compose project"
}

func (m *Monitor) probeProcess(svc *db.Service) (bool, string) {
	// First check in-memory procs map (authoritative for this session)
	m.mu.RLock()
	cmd, inMap := m.procs[svc.ID]
	m.mu.RUnlock()
	if inMap && cmd.Process != nil {
		p, err := process.NewProcess(int32(cmd.Process.Pid))
		if err == nil {
			running, err := p.IsRunning()
			if err == nil && running {
				return true, "OK"
			}
		}
		// Process in map but dead — clean up
		m.mu.Lock()
		delete(m.procs, svc.ID)
		m.mu.Unlock()
		return false, "process exited"
	}
	// Fall back to DB-stored PID (e.g. after monitor restart)
	if svc.PID > 0 {
		p, err := process.NewProcess(int32(svc.PID))
		if err != nil {
			return false, "pid not found"
		}
		running, err := p.IsRunning()
		if err != nil || !running {
			return false, "process not running"
		}
		return true, "OK"
	}
	// Nothing tracked — only restart if status was previously running
	if svc.Status == db.StatusRunning {
		return false, "no pid tracked"
	}
	// Freshly deployed and never started — don't auto-restart, just report stopped
	return false, "not started"
}

func (m *Monitor) probeSystemd(svc *db.Service) (bool, string) {
	unit := svc.SystemdUnit
	if unit == "" {
		unit = svc.Name + ".service"
	}
	out, err := exec.CommandContext(m.ctx, "systemctl", "is-active", unit).Output()
	if err != nil {
		return false, "systemctl error"
	}
	state := strings.TrimSpace(string(out))
	if state == "active" {
		return true, "OK"
	}
	return false, "unit state: " + state
}

// ─── Auto-Restart ─────────────────────────────────────────────────────────────

func (m *Monitor) handleUnhealthy(svc *db.Service) {
	if svc.RestartCount >= m.cfg.Monitor.MaxRestarts {
		log.Printf("monitor: %s exceeded max restarts (%d), giving up", svc.Name, m.cfg.Monitor.MaxRestarts)
		m.db.RecordEvent(svc.ID, db.EventError, fmt.Sprintf("exceeded max restarts (%d)", m.cfg.Monitor.MaxRestarts))
		return
	}

	log.Printf("monitor: restarting %s (attempt %d)", svc.Name, svc.RestartCount+1)
	m.db.IncrementRestartCount(svc.ID)
	m.db.RecordEvent(svc.ID, db.EventRestart, "auto-restart triggered")
	m.broadcast(EventBroadcast{
		ServiceID: svc.ID,
		Type:      "restart",
		Message:   fmt.Sprintf("auto-restarting (attempt %d)", svc.RestartCount+1),
		Time:      time.Now(),
	})

	if err := m.RestartService(svc); err != nil {
		log.Printf("monitor: restart %s failed: %v", svc.Name, err)
		m.db.RecordEvent(svc.ID, db.EventError, "restart failed: "+err.Error())
		m.db.UpdateServiceStatus(svc.ID, db.StatusError, 0)
	}
}

// RestartService is exported for manual restarts from the API/TUI
func (m *Monitor) RestartService(svc *db.Service) error {
	switch svc.Type {
	case db.ServiceTypeDocker:
		return m.restartDocker(svc)
	case db.ServiceTypeCompose:
		return m.restartCompose(svc)
	case db.ServiceTypeProcess:
		return m.restartProcess(svc)
	case db.ServiceTypeSystemd:
		return m.restartSystemd(svc)
	default:
		return fmt.Errorf("unsupported restart for type %s", svc.Type)
	}
}

func (m *Monitor) StartService(svc *db.Service) error {
	m.db.UpdateServiceStatus(svc.ID, db.StatusStarting, 0)
	m.db.RecordEvent(svc.ID, db.EventStart, "manual start")
	var err error
	switch svc.Type {
	case db.ServiceTypeDocker:
		err = m.startDocker(svc)
	case db.ServiceTypeCompose:
		err = m.startCompose(svc)
	case db.ServiceTypeProcess:
		err = m.startProcess(svc)
	case db.ServiceTypeSystemd:
		err = exec.Command("systemctl", "start", svc.SystemdUnit).Run()
	case db.ServiceTypeStatic:
		err = m.startStatic(svc)
	default:
		err = fmt.Errorf("cannot start type %s", svc.Type)
	}
	if err != nil {
		m.db.UpdateServiceStatus(svc.ID, db.StatusError, 0)
		return err
	}
	m.db.SetServiceStarted(svc.ID)
	return nil
}

func (m *Monitor) StopService(svc *db.Service) error {
	m.db.RecordEvent(svc.ID, db.EventStop, "manual stop")
	var err error
	switch svc.Type {
	case db.ServiceTypeDocker:
		name := svc.ContainerName
		if name == "" {
			name = svc.Name
		}
		err = exec.Command("docker", "stop", name).Run()
	case db.ServiceTypeCompose:
		err = exec.Command("docker", "compose", "-f", svc.ComposeFile, "stop").Run()
	case db.ServiceTypeProcess:
		m.mu.Lock()
		cmd, ok := m.procs[svc.ID]
		if ok && cmd.Process != nil {
			syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			cmd.Process.Signal(os.Interrupt)
		}
		delete(m.procs, svc.ID)
		m.mu.Unlock()
	case db.ServiceTypeStatic:
		m.mu.Lock()
		if cmd, ok := m.procs[svc.ID]; ok && cmd.Process != nil {
			cmd.Process.Kill()
		}
		delete(m.procs, svc.ID)
		m.mu.Unlock()
	case db.ServiceTypeSystemd:
		err = exec.Command("systemctl", "stop", svc.SystemdUnit).Run()
	}
	if err == nil {
		m.db.UpdateServiceStatus(svc.ID, db.StatusStopped, 0)
	}
	return err
}

func (m *Monitor) startDocker(svc *db.Service) error {
	name := svc.ContainerName
	if name == "" {
		name = svc.Name
	}
	// Try start first; if container doesn't exist, do run
	if err := exec.Command("docker", "start", name).Run(); err != nil {
		// Build docker run args
		args := []string{"run", "-d", "--name", name}
		for _, p := range strings.Split(svc.Ports, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				args = append(args, "-p", p)
			}
		}
		for _, v := range strings.Split(svc.Volumes, ",") {
			v = strings.TrimSpace(v)
			if v != "" {
				args = append(args, "-v", v)
			}
		}
		args = append(args, svc.Image)
		return exec.Command("docker", args...).Run()
	}
	return nil
}

func (m *Monitor) restartDocker(svc *db.Service) error {
	name := svc.ContainerName
	if name == "" {
		name = svc.Name
	}
	return exec.Command("docker", "restart", name).Run()
}

func (m *Monitor) startCompose(svc *db.Service) error {
	cf := svc.ComposeFile
	if cf == "" {
		cf = "docker-compose.yml"
	}
	return exec.Command("docker", "compose", "-f", cf, "up", "-d").Run()
}

func (m *Monitor) restartCompose(svc *db.Service) error {
	cf := svc.ComposeFile
	if cf == "" {
		cf = "docker-compose.yml"
	}
	return exec.Command("docker", "compose", "-f", cf, "restart").Run()
}

func (m *Monitor) startProcess(svc *db.Service) error {
	if svc.Command == "" {
		return fmt.Errorf("no command configured for process service")
	}
	// Run via bash so PATH, shell aliases, and npm scripts all resolve correctly
	cmd := exec.Command("bash", "-c", svc.Command)
	if svc.WorkingDir != "" {
		cmd.Dir = svc.WorkingDir
	}
	// Inherit the parent environment so node/npm/python etc are on PATH
	cmd.Env = append(os.Environ())
	// Put the process in its own group so we can kill all children (e.g. npm spawning node)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	m.mu.Lock()
	m.procs[svc.ID] = cmd
	m.mu.Unlock()
	m.db.UpdateServiceStatus(svc.ID, db.StatusRunning, pid)
	go func() {
		cmd.Wait()
		m.mu.Lock()
		delete(m.procs, svc.ID)
		m.mu.Unlock()
		m.db.UpdateServiceStatus(svc.ID, db.StatusStopped, 0)
		m.broadcast(EventBroadcast{ServiceID: svc.ID, Type: "crash", Message: "process exited", Time: time.Now()})
	}()
	return nil
}

func (m *Monitor) restartProcess(svc *db.Service) error {
	m.mu.Lock()
	if cmd, ok := m.procs[svc.ID]; ok && cmd.Process != nil {
		// Kill the entire process group (catches npm-spawned node, etc.)
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Process.Kill()
	}
	delete(m.procs, svc.ID)
	m.mu.Unlock()
	time.Sleep(500 * time.Millisecond)
	return m.startProcess(svc)
}

func (m *Monitor) restartSystemd(svc *db.Service) error {
	unit := svc.SystemdUnit
	if unit == "" {
		unit = svc.Name + ".service"
	}
	return exec.Command("systemctl", "restart", unit).Run()
}

// ─── System Metrics Loop ─────────────────────────────────────────────────────

func (m *Monitor) systemMetricsLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.collectSystemMetrics()
		}
	}
}

func (m *Monitor) collectSystemMetrics() {
	cpus, _ := cpu.Percent(0, false)
	vmem, _ := mem.VirtualMemory()
	dsk, _ := disk.Usage("/")
	ld, _ := load.Avg()

	sm := &db.SystemMetric{}
	if len(cpus) > 0 {
		sm.CPU = cpus[0]
	}
	if vmem != nil {
		sm.MemTotal = vmem.Total
		sm.MemUsed = vmem.Used
	}
	if dsk != nil {
		sm.DiskTotal = dsk.Total
		sm.DiskUsed = dsk.Used
	}
	if ld != nil {
		sm.LoadAvg = ld.Load1
	}
	m.db.RecordSystemMetric(sm)
}

// GetSystemStats returns the latest system snapshot
func (m *Monitor) GetSystemStats() map[string]interface{} {
	cpus, _ := cpu.Percent(0, false)
	vmem, _ := mem.VirtualMemory()
	dsk, _ := disk.Usage("/")
	ld, _ := load.Avg()

	stats := map[string]interface{}{}
	if len(cpus) > 0 {
		stats["cpu"] = fmt.Sprintf("%.1f", cpus[0])
	}
	if vmem != nil {
		stats["mem_total"] = vmem.Total
		stats["mem_used"] = vmem.Used
		stats["mem_pct"] = fmt.Sprintf("%.1f", vmem.UsedPercent)
	}
	if dsk != nil {
		stats["disk_total"] = dsk.Total
		stats["disk_used"] = dsk.Used
		stats["disk_pct"] = fmt.Sprintf("%.1f", dsk.UsedPercent)
	}
	if ld != nil {
		stats["load"] = fmt.Sprintf("%.2f", ld.Load1)
	}
	return stats
}

// ─── Prune Loop ──────────────────────────────────────────────────────────────

func (m *Monitor) pruneLoop() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			d := time.Duration(m.cfg.Monitor.EventsRetentionDays) * 24 * time.Hour
			m.db.PruneEvents(d)
			md := time.Duration(m.cfg.Monitor.MetricsRetentionDays) * 24 * time.Hour
			m.db.PruneMetrics(md)
		}
	}
}

// ReloadInterval restarts the health loop with a new interval without
// stopping the entire monitor (subscribers and system metrics loop keep running).
func (m *Monitor) ReloadInterval(d time.Duration) {
	m.mu.Lock()
	m.cfg.Monitor.Interval = d
	m.mu.Unlock()
	log.Printf("monitor: interval updated to %s", d)
	// The healthLoop reads cfg.Monitor.Interval each tick reset via
	// a dedicated reload channel approach — simplest is to signal via cancel/restart
	// We restart only the health goroutine by using a sub-context trick.
	// For simplicity: next ticker cycle will use the new value if we reset the ticker.
	// Since Go tickers don't support reset on <1.15, we signal via reloadCh.
	select {
	case m.reloadCh <- d:
	default:
	}
}

// GetHostInfo returns static host metadata
func (m *Monitor) GetHostInfo() map[string]interface{} {
	info := map[string]interface{}{}

	if hi, err := host.Info(); err == nil {
		info["hostname"] = hi.Hostname
		info["os"] = hi.OS
		info["platform"] = hi.Platform
		info["platform_version"] = hi.PlatformVersion
		info["kernel"] = hi.KernelVersion
		info["arch"] = hi.KernelArch
		info["uptime"] = hi.Uptime
		info["boot_time"] = hi.BootTime
		info["procs"] = hi.Procs
		info["virtualization"] = hi.VirtualizationSystem
	}

	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		info["cpu_model"] = cpuInfo[0].ModelName
		info["cpu_cores"] = cpuInfo[0].Cores
	}
	if counts, err := cpu.Counts(true); err == nil {
		info["cpu_threads"] = counts
	}

	if vmem, err := mem.VirtualMemory(); err == nil {
		info["mem_total"] = vmem.Total
		info["mem_used"] = vmem.Used
		info["mem_free"] = vmem.Free
		info["mem_pct"] = fmt.Sprintf("%.1f", vmem.UsedPercent)
	}

	if dsk, err := disk.Usage("/"); err == nil {
		info["disk_total"] = dsk.Total
		info["disk_used"] = dsk.Used
		info["disk_free"] = dsk.Free
		info["disk_pct"] = fmt.Sprintf("%.1f", dsk.UsedPercent)
	}

	if partitions, err := disk.Partitions(false); err == nil {
		var parts []map[string]interface{}
		for _, p := range partitions {
			u, err := disk.Usage(p.Mountpoint)
			if err != nil {
				continue
			}
			parts = append(parts, map[string]interface{}{
				"device":     p.Device,
				"mountpoint": p.Mountpoint,
				"fstype":     p.Fstype,
				"total":      u.Total,
				"used":       u.Used,
				"free":       u.Free,
				"pct":        fmt.Sprintf("%.1f", u.UsedPercent),
			})
		}
		info["partitions"] = parts
	}

	return info
}

// fmtBytes converts bytes to a human-readable string
func FmtBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ─── Static file server ───────────────────────────────────────────────────────

func (m *Monitor) probeStatic(svc *db.Service) (bool, string) {
	port := svc.StaticPort
	if port == 0 {
		port = 8080
	}
	// Check if our server proc is alive
	m.mu.RLock()
	cmd, ok := m.procs[svc.ID]
	m.mu.RUnlock()
	if ok && cmd.Process != nil {
		// Quick TCP dial to confirm it's accepting
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 2*time.Second)
		if err == nil {
			conn.Close()
			return true, "OK"
		}
		return false, "port not responding"
	}
	return false, "no pid tracked"
}

func (m *Monitor) startStatic(svc *db.Service) error {
	dir := svc.StaticDir
	if dir == "" {
		dir = "."
	}
	port := svc.StaticPort
	if port == 0 {
		port = 8080
	}
	addr := ":" + strconv.Itoa(port)

	// Use Python's built-in http.server if available, else Go's own http.FileServer
	// Try Python 3 first (available on most Linux servers)
	pythonCmd := fmt.Sprintf("python3 -m http.server %d --directory '%s'", port, dir)
	cmd := exec.Command("bash", "-c", pythonCmd)
	cmd.Env = append(os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		// Fall back: spawn a goroutine with Go's own file server
		return m.startGoFileServer(svc, dir, addr)
	}

	m.mu.Lock()
	m.procs[svc.ID] = cmd
	m.mu.Unlock()
	m.db.UpdateServiceStatus(svc.ID, db.StatusRunning, cmd.Process.Pid)
	go func() {
		cmd.Wait()
		m.mu.Lock()
		delete(m.procs, svc.ID)
		m.mu.Unlock()
		m.db.UpdateServiceStatus(svc.ID, db.StatusStopped, 0)
		m.broadcast(EventBroadcast{ServiceID: svc.ID, Type: "crash", Message: "static server exited", Time: time.Now()})
	}()
	return nil
}

func (m *Monitor) startGoFileServer(svc *db.Service, dir, addr string) error {
	// Store a sentinel in procs so probeStatic can see it
	// The actual listener runs in a goroutine
	srv := &http.Server{Addr: addr, Handler: http.FileServer(http.Dir(dir))}
	log.Printf("monitor: starting Go file server for %s at %s serving %s", svc.Name, addr, dir)
	m.db.UpdateServiceStatus(svc.ID, db.StatusRunning, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			m.db.UpdateServiceStatus(svc.ID, db.StatusError, 0)
			m.broadcast(EventBroadcast{ServiceID: svc.ID, Type: "crash", Message: "file server error: " + err.Error(), Time: time.Now()})
		}
	}()
	return nil
}

func (m *Monitor) restartStatic(svc *db.Service) error {
	m.mu.Lock()
	if cmd, ok := m.procs[svc.ID]; ok && cmd.Process != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Process.Kill()
	}
	delete(m.procs, svc.ID)
	m.mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	return m.startStatic(svc)
}
