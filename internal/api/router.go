package api

import (
	"log"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/pikostack/pikostack/internal/api/handlers"
	"github.com/pikostack/pikostack/internal/api/middleware"
	"github.com/pikostack/pikostack/internal/config"
	"github.com/pikostack/pikostack/internal/db"
	"github.com/pikostack/pikostack/internal/monitor"
)

func NewRouter(database *db.Database, mon *monitor.Monitor, cfg *config.Config) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(cors.Default())

	h := handlers.New(database, mon, cfg)

	if err := h.SetFS(handlers.GetTemplateFS()); err != nil {
		log.Fatalf("template init: %v", err)
	}

	r.GET("/static/*filepath", handlers.StaticHandler())

	var authMW gin.HandlerFunc = func(c *gin.Context) { c.Next() }
	if cfg.Auth.Enabled {
		authMW = middleware.BasicAuth(cfg.Auth.Username, cfg.Auth.Password)
	}

	web := r.Group("/", authMW)
	{
		web.GET("/", h.PageDashboard)
		web.GET("/services", h.PageServices)
		web.GET("/services/:id", h.PageServiceDetail)
		web.GET("/deploy", h.PageDeploy)
		web.GET("/analytics", h.PageAnalytics)
		web.GET("/settings", h.PageSettings)
	}

	htmx := r.Group("/htmx", authMW)
	{
		htmx.GET("/dashboard/stats", h.HtmxDashboardStats)
		htmx.GET("/services/list", h.HtmxServiceList)
		htmx.GET("/services/:id/card", h.HtmxServiceCard)
		htmx.GET("/events/feed", h.HtmxEventFeed)
	}

	api := r.Group("/api/v1", authMW)
	{
		api.GET("/projects", h.ListProjects)
		api.POST("/projects", h.CreateProject)
		api.GET("/projects/:id", h.GetProject)
		api.DELETE("/projects/:id", h.DeleteProject)

		api.GET("/services", h.ListServices)
		api.POST("/services", h.CreateService)
		api.GET("/services/:id", h.GetService)
		api.PUT("/services/:id", h.UpdateService)
		api.DELETE("/services/:id", h.DeleteService)
		api.POST("/services/:id/start", h.StartService)
		api.POST("/services/:id/stop", h.StopService)
		api.POST("/services/:id/restart", h.RestartService)
		api.GET("/services/:id/events", h.ListServiceEvents)
		api.GET("/services/:id/metrics", h.ListServiceMetrics)

		api.GET("/analytics/overview", h.AnalyticsOverview)
		api.GET("/analytics/system", h.AnalyticsSystem)
		api.GET("/events", h.ListEvents)
		api.GET("/system/stats", h.SystemStats)
	}

	r.GET("/ws/events", authMW, h.WSEvents)
	r.GET("/ws/logs/:id", authMW, h.WSLogs)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "app": "pikostack"})
	})

	return r
}
