package handlers

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pikostack/pikostack/internal/config"
	"github.com/pikostack/pikostack/internal/db"
	"github.com/pikostack/pikostack/internal/monitor"
)

type Handler struct {
	db          *db.Database
	mon         *monitor.Monitor
	cfg         *config.Config
	baseFS      fs.FS
	funcMap     template.FuncMap
	baseSources []templateSource
}

type templateSource struct {
	name    string
	content string
}

func New(database *db.Database, mon *monitor.Monitor, cfg *config.Config) *Handler {
	return &Handler{db: database, mon: mon, cfg: cfg}
}

func (h *Handler) SetTemplates(_ *template.Template) {}

func (h *Handler) SetFS(fsys fs.FS) error {
	h.baseFS = fsys
	h.funcMap = buildFuncMap()
	return fs.WalkDir(fsys, "web/templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".html" {
			return err
		}
		name := strings.TrimPrefix(path, "web/templates/")
		if strings.HasPrefix(name, "pages/") {
			return nil
		}
		content, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		h.baseSources = append(h.baseSources, templateSource{name: name, content: string(content)})
		return nil
	})
}

func (h *Handler) pageTemplate(pageName string) (*template.Template, error) {
	t := template.New("").Funcs(h.funcMap)
	for _, src := range h.baseSources {
		if _, err := t.New(src.name).Parse(src.content); err != nil {
			return nil, err
		}
	}
	path := "web/templates/" + pageName
	content, err := fs.ReadFile(h.baseFS, path)
	if err != nil {
		return nil, err
	}
	if _, err := t.New(pageName).Parse(string(content)); err != nil {
		return nil, err
	}
	return t, nil
}

func (h *Handler) render(c *gin.Context, pageName string, data gin.H) {
	if data == nil {
		data = gin.H{}
	}
	data["AppName"] = "Pikostack"
	data["ViewName"] = "Pikoview"
	data["CurrentPath"] = c.Request.URL.Path
	data["AuthUser"] = h.cfg.Auth.Username
	data["AuthEnabled"] = h.cfg.Auth.Enabled

	// Hostname for navbar
	info := h.mon.GetHostInfo()
	data["Hostname"] = info["hostname"]
	data["HostOS"] = info["os"]

	t, err := h.pageTemplate(pageName)
	if err != nil {
		c.String(http.StatusInternalServerError, "template load error: %v", err)
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(c.Writer, "layouts/base.html", data); err != nil {
		c.String(http.StatusInternalServerError, "template render error: %v", err)
	}
}

func (h *Handler) renderPartial(c *gin.Context, tmplName string, data interface{}) {
	t := template.New("").Funcs(h.funcMap)
	for _, src := range h.baseSources {
		if _, err := t.New(src.name).Parse(src.content); err != nil {
			c.String(http.StatusInternalServerError, "partial load error: %v", err)
			return
		}
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(c.Writer, tmplName, data); err != nil {
		c.String(http.StatusInternalServerError, "partial render error: %v", err)
	}
}

func (h *Handler) ServeStatic(c *gin.Context) { c.Status(http.StatusNotFound) }

func LoadTemplates(fsys fs.FS) (*template.Template, error) {
	return template.New("placeholder").Parse("")
}

// str converts any named string type (ServiceType, ServiceStatus, etc.) to string
func str(v interface{}) string { return fmt.Sprint(v) }

func buildFuncMap() template.FuncMap {
	return template.FuncMap{
		"statusColor": func(v interface{}) string {
			switch str(v) {
			case "running":
				return "text-emerald-400"
			case "stopped":
				return "text-slate-400"
			case "error":
				return "text-rose-400"
			case "starting":
				return "text-amber-400"
			default:
				return "text-slate-400"
			}
		},
		"statusBg": func(v interface{}) string {
			switch str(v) {
			case "running":
				return "bg-emerald-500/20 text-emerald-400 border border-emerald-500/30"
			case "stopped":
				return "bg-slate-500/20 text-slate-400 border border-slate-500/30"
			case "error":
				return "bg-rose-500/20 text-rose-400 border border-rose-500/30"
			case "starting":
				return "bg-amber-500/20 text-amber-400 border border-amber-500/30"
			default:
				return "bg-slate-500/20 text-slate-400 border border-slate-500/30"
			}
		},
		"statusDot": func(v interface{}) string {
			switch str(v) {
			case "running":
				return "bg-emerald-400"
			case "error":
				return "bg-rose-400"
			case "starting":
				return "bg-amber-400"
			default:
				return "bg-slate-500"
			}
		},
		"statusLabel": func(v interface{}) string { return str(v) },
		"typeStr":     func(v interface{}) string { return str(v) },
		"eventIcon": func(v interface{}) string {
			switch str(v) {
			case "start":
				return "▶"
			case "stop":
				return "■"
			case "restart":
				return "↻"
			case "crash":
				return "✕"
			case "deploy":
				return "⬆"
			case "health_check":
				return "♥"
			default:
				return "•"
			}
		},
		"typeIcon": func(v interface{}) string {
			switch str(v) {
			case "docker":
				return "🐳"
			case "compose":
				return "🐙"
			case "process":
				return "⚙"
			case "systemd":
				return "🔧"
			case "url":
				return "🌐"
			case "static":
				return "📁"
			default:
				return "📦"
			}
		},
		"typeBadge": func(v interface{}) string {
			switch str(v) {
			case "docker":
				return "bg-sky-500/20 text-sky-400 border border-sky-500/30"
			case "compose":
				return "bg-indigo-500/20 text-indigo-400 border border-indigo-500/30"
			case "process":
				return "bg-orange-500/20 text-orange-400 border border-orange-500/30"
			case "systemd":
				return "bg-teal-500/20 text-teal-400 border border-teal-500/30"
			case "url":
				return "bg-violet-500/20 text-violet-400 border border-violet-500/30"
			case "static":
				return "bg-emerald-500/20 text-emerald-400 border border-emerald-500/30"
			default:
				return "bg-slate-500/20 text-slate-400 border border-slate-500/30"
			}
		},
		"mul": func(a, b float64) float64 { return a * b },
		"divf": func(a, b int64) float64 {
			if b == 0 {
				return 0
			}
			return float64(a) / float64(b)
		},
		"add": func(a, b int) int { return a + b },
		"trimPath": func(s string) string {
			parts := strings.Split(s, "/")
			if len(parts) > 0 {
				return parts[len(parts)-1]
			}
			return s
		},
		"isActive": func(current, target string) string {
			if current == target || strings.HasPrefix(current, target+"/") {
				return "active"
			}
			return ""
		},
		"hasPrefix": func(s, prefix string) bool {
			return strings.HasPrefix(s, prefix)
		},
		"slice": func(args ...string) []string { return args },
	}
}
