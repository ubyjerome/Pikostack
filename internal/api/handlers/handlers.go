package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/pikostack/pikostack/internal/config"
	"github.com/pikostack/pikostack/internal/db"
)

func (h *Handler) PageDashboard(c *gin.Context) {
	services, _ := h.db.ListServices()
	summary, _ := h.db.GetServiceSummary()
	events, _ := h.db.ListEvents("", 20)
	stats := h.mon.GetSystemStats()

	h.render(c, "pages/dashboard.html", gin.H{
		"Title":    "Dashboard",
		"Services": services,
		"Summary":  summary,
		"Events":   events,
		"Stats":    stats,
	})
}

func (h *Handler) PageServices(c *gin.Context) {
	services, _ := h.db.ListServices()
	projects, _ := h.db.ListProjects()
	h.render(c, "pages/services.html", gin.H{
		"Title":    "Services",
		"Services": services,
		"Projects": projects,
	})
}

func (h *Handler) PageServiceDetail(c *gin.Context) {
	id := c.Param("id")
	svc, err := h.db.GetService(id)
	if err != nil {
		c.Redirect(http.StatusFound, "/services")
		return
	}
	events, _ := h.db.ListEvents(id, 50)
	metrics, _ := h.db.ListMetrics(id, time.Now().Add(-24*time.Hour), 200)
	h.render(c, "pages/service_detail.html", gin.H{
		"Title":   svc.Name,
		"Service": svc,
		"Events":  events,
		"Metrics": metrics,
	})
}

func (h *Handler) PageDeploy(c *gin.Context) {
	projects, _ := h.db.ListProjects()
	h.render(c, "pages/deploy.html", gin.H{
		"Title":    "Deploy",
		"Projects": projects,
	})
}

func (h *Handler) PageAnalytics(c *gin.Context) {
	services, _ := h.db.ListServices()
	summary, _ := h.db.GetServiceSummary()
	sysMetrics, _ := h.db.ListSystemMetrics(time.Now().Add(-24 * time.Hour))
	h.render(c, "pages/analytics.html", gin.H{
		"Title":      "Analytics",
		"Services":   services,
		"Summary":    summary,
		"SysMetrics": sysMetrics,
	})
}

func (h *Handler) PageSettings(c *gin.Context) {
	projects, _ := h.db.ListProjects()
	h.render(c, "pages/settings.html", gin.H{
		"Title":    "Settings",
		"Cfg":      h.cfg,
		"Projects": projects,
	})
}

// ─── HTMX Partials ───────────────────────────────────────────────────────────

func (h *Handler) HtmxDashboardStats(c *gin.Context) {
	summary, _ := h.db.GetServiceSummary()
	stats := h.mon.GetSystemStats()
	h.renderPartial(c, "partials/dashboard_stats.html", gin.H{
		"Summary": summary,
		"Stats":   stats,
	})
}

func (h *Handler) HtmxServiceList(c *gin.Context) {
	services, _ := h.db.ListServices()
	h.renderPartial(c, "partials/service_list.html", services)
}

func (h *Handler) HtmxServiceCard(c *gin.Context) {
	id := c.Param("id")
	svc, err := h.db.GetService(id)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	h.renderPartial(c, "partials/service_card.html", svc)
}

func (h *Handler) HtmxEventFeed(c *gin.Context) {
	events, _ := h.db.ListEvents("", 30)
	h.renderPartial(c, "partials/event_feed.html", events)
}

// ─── REST: Projects ──────────────────────────────────────────────────────────

func (h *Handler) ListProjects(c *gin.Context) {
	projects, err := h.db.ListProjects()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, projects)
}

func (h *Handler) CreateProject(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := h.db.CreateProject(req.Name, req.Description)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (h *Handler) GetProject(c *gin.Context) {
	p, err := h.db.GetProject(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) DeleteProject(c *gin.Context) {
	if err := h.db.DeleteProject(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// ─── REST: Services ──────────────────────────────────────────────────────────

func (h *Handler) ListServices(c *gin.Context) {
	services, err := h.db.ListServices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, services)
}

func (h *Handler) CreateService(c *gin.Context) {
	var svc db.Service
	if err := c.ShouldBindJSON(&svc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.db.CreateService(&svc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.db.RecordEvent(svc.ID, db.EventDeploy, "service registered")
	c.JSON(http.StatusCreated, svc)
}

func (h *Handler) GetService(c *gin.Context) {
	svc, err := h.db.GetService(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, svc)
}

func (h *Handler) UpdateService(c *gin.Context) {
	svc, err := h.db.GetService(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err := c.ShouldBindJSON(svc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.db.UpdateService(svc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, svc)
}

func (h *Handler) DeleteService(c *gin.Context) {
	if err := h.db.DeleteService(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) StartService(c *gin.Context) {
	svc, err := h.db.GetService(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err := h.mon.StartService(svc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "started"})
}

func (h *Handler) StopService(c *gin.Context) {
	svc, err := h.db.GetService(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err := h.mon.StopService(svc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

func (h *Handler) RestartService(c *gin.Context) {
	svc, err := h.db.GetService(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	h.db.RecordEvent(svc.ID, db.EventRestart, "manual restart")
	if err := h.mon.RestartService(svc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "restarted"})
}

func (h *Handler) ListServiceEvents(c *gin.Context) {
	events, err := h.db.ListEvents(c.Param("id"), 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, events)
}

func (h *Handler) ListServiceMetrics(c *gin.Context) {
	since := time.Now().Add(-24 * time.Hour)
	metrics, err := h.db.ListMetrics(c.Param("id"), since, 500)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, metrics)
}

// ─── REST: Analytics + System ─────────────────────────────────────────────────

func (h *Handler) AnalyticsOverview(c *gin.Context) {
	summary, _ := h.db.GetServiceSummary()
	events, _ := h.db.ListEvents("", 50)
	c.JSON(http.StatusOK, gin.H{
		"summary": summary,
		"events":  events,
	})
}

func (h *Handler) AnalyticsSystem(c *gin.Context) {
	metrics, _ := h.db.ListSystemMetrics(time.Now().Add(-24 * time.Hour))
	c.JSON(http.StatusOK, metrics)
}

func (h *Handler) ListEvents(c *gin.Context) {
	events, err := h.db.ListEvents("", 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, events)
}

func (h *Handler) SystemStats(c *gin.Context) {
	c.JSON(http.StatusOK, h.mon.GetSystemStats())
}

// ─── Settings API ─────────────────────────────────────────────────────────────

func (h *Handler) GetSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"server": gin.H{
			"host":   h.cfg.Server.Host,
			"port":   h.cfg.Server.Port,
		},
		"auth": gin.H{
			"enabled":  h.cfg.Auth.Enabled,
			"username": h.cfg.Auth.Username,
		},
		"monitor": gin.H{
			"interval":               h.cfg.Monitor.Interval.String(),
			"grace_period":           h.cfg.Monitor.GracePeriod.String(),
			"max_restarts":           h.cfg.Monitor.MaxRestarts,
			"metrics_retention_days": h.cfg.Monitor.MetricsRetentionDays,
			"events_retention_days":  h.cfg.Monitor.EventsRetentionDays,
		},
		"database": gin.H{
			"path": h.cfg.Database.Path,
		},
	})
}

func (h *Handler) SaveSettings(c *gin.Context) {
	var req struct {
		Monitor struct {
			Interval             string `json:"interval"`
			GracePeriod          string `json:"grace_period"`
			MaxRestarts          int    `json:"max_restarts"`
			MetricsRetentionDays int    `json:"metrics_retention_days"`
			EventsRetentionDays  int    `json:"events_retention_days"`
		} `json:"monitor"`
		Auth struct {
			Enabled  bool   `json:"enabled"`
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"auth"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	changed := false

	// Monitor interval
	if req.Monitor.Interval != "" {
		d, err := time.ParseDuration(req.Monitor.Interval)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid interval: " + err.Error()})
			return
		}
		if d < 5*time.Second {
			c.JSON(http.StatusBadRequest, gin.H{"error": "interval must be at least 5s"})
			return
		}
		h.cfg.Monitor.Interval = d
		h.mon.ReloadInterval(d)
		changed = true
	}

	if req.Monitor.GracePeriod != "" {
		d, err := time.ParseDuration(req.Monitor.GracePeriod)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid grace_period: " + err.Error()})
			return
		}
		h.cfg.Monitor.GracePeriod = d
		changed = true
	}

	if req.Monitor.MaxRestarts > 0 {
		h.cfg.Monitor.MaxRestarts = req.Monitor.MaxRestarts
		changed = true
	}

	if req.Monitor.MetricsRetentionDays > 0 {
		h.cfg.Monitor.MetricsRetentionDays = req.Monitor.MetricsRetentionDays
		changed = true
	}

	if req.Monitor.EventsRetentionDays > 0 {
		h.cfg.Monitor.EventsRetentionDays = req.Monitor.EventsRetentionDays
		changed = true
	}

	// Auth settings
	h.cfg.Auth.Enabled = req.Auth.Enabled
	if req.Auth.Username != "" {
		h.cfg.Auth.Username = req.Auth.Username
	}
	if req.Auth.Password != "" {
		h.cfg.Auth.Password = req.Auth.Password
	}
	changed = true

	if changed {
		if err := persistConfig(h.cfg); err != nil {
			// Non-fatal: settings applied in memory, just warn
			c.JSON(http.StatusOK, gin.H{
				"status":  "applied",
				"warning": "could not write to config file: " + err.Error(),
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "saved"})
}

func persistConfig(cfg *config.Config) error {
	viper.Set("server.host", cfg.Server.Host)
	viper.Set("server.port", cfg.Server.Port)
	viper.Set("auth.enabled", cfg.Auth.Enabled)
	viper.Set("auth.username", cfg.Auth.Username)
	viper.Set("auth.password", cfg.Auth.Password)
	viper.Set("monitor.interval", cfg.Monitor.Interval.String())
	viper.Set("monitor.grace_period", cfg.Monitor.GracePeriod.String())
	viper.Set("monitor.max_restarts", cfg.Monitor.MaxRestarts)
	viper.Set("monitor.metrics_retention_days", cfg.Monitor.MetricsRetentionDays)
	viper.Set("monitor.events_retention_days", cfg.Monitor.EventsRetentionDays)
	viper.Set("database.path", cfg.Database.Path)
	return viper.WriteConfig()
}
