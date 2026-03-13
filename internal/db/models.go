package db

import (
	"time"
)

// ServiceType defines how Pikostack manages the service
type ServiceType string

const (
	ServiceTypeDocker   ServiceType = "docker"
	ServiceTypeCompose  ServiceType = "compose"
	ServiceTypeProcess  ServiceType = "process"
	ServiceTypeSystemd  ServiceType = "systemd"
	ServiceTypeURL      ServiceType = "url" // watchdog only, no restart
)

// ServiceStatus is the last known health state
type ServiceStatus string

const (
	StatusRunning  ServiceStatus = "running"
	StatusStopped  ServiceStatus = "stopped"
	StatusError    ServiceStatus = "error"
	StatusStarting ServiceStatus = "starting"
	StatusUnknown  ServiceStatus = "unknown"
)

// EventType records what happened to a service
type EventType string

const (
	EventStart       EventType = "start"
	EventStop        EventType = "stop"
	EventRestart     EventType = "restart"
	EventCrash       EventType = "crash"
	EventHealthCheck EventType = "health_check"
	EventDeploy      EventType = "deploy"
	EventError       EventType = "error"
)

// Project groups related services
type Project struct {
	ID          string    `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;not null" json:"name"`
	Description string    `json:"description"`
	Services    []Service `gorm:"foreignKey:ProjectID" json:"services,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Service is a managed workload
type Service struct {
	ID          string        `gorm:"primaryKey" json:"id"`
	ProjectID   string        `gorm:"index" json:"project_id"`
	Project     *Project      `gorm:"foreignKey:ProjectID" json:"project,omitempty"`
	Name        string        `gorm:"not null" json:"name"`
	Description string        `json:"description"`
	Type        ServiceType   `gorm:"not null" json:"type"`
	Status      ServiceStatus `gorm:"default:unknown" json:"status"`
	AutoRestart bool          `gorm:"default:true" json:"auto_restart"`
	WatchOnly   bool          `gorm:"default:false" json:"watch_only"`

	// Docker fields
	Image         string `json:"image,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
	ContainerID   string `json:"container_id,omitempty"`
	Ports         string `json:"ports,omitempty"`    // "8080:80,443:443"
	EnvVars       string `json:"env_vars,omitempty"` // JSON encoded map
	Volumes       string `json:"volumes,omitempty"`  // "host:container,..."
	Networks      string `json:"networks,omitempty"`
	ComposeFile   string `json:"compose_file,omitempty"` // path to compose file

	// Process fields
	Command    string `json:"command,omitempty"`
	WorkingDir string `json:"working_dir,omitempty"`
	PID        int    `json:"pid,omitempty"`

	// Systemd fields
	SystemdUnit string `json:"systemd_unit,omitempty"`

	// URL watchdog fields
	HealthURL        string `json:"health_url,omitempty"`
	HealthMethod     string `json:"health_method,omitempty"`      // GET, HEAD
	HealthStatusCode int    `json:"health_status_code,omitempty"` // expected code

	// Runtime tracking
	RestartCount   int        `json:"restart_count"`
	LastHealthAt   *time.Time `json:"last_health_at,omitempty"`
	LastHealthOK   bool       `json:"last_health_ok"`
	UptimeSeconds  int64      `json:"uptime_seconds"`
	StartedAt      *time.Time `json:"started_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ServiceEvent records a lifecycle event
type ServiceEvent struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	ServiceID string    `gorm:"index;not null" json:"service_id"`
	Service   *Service  `gorm:"foreignKey:ServiceID" json:"service,omitempty"`
	Type      EventType `gorm:"not null" json:"type"`
	Message   string    `json:"message"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// Metric is a point-in-time resource snapshot
type Metric struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ServiceID string    `gorm:"index;not null" json:"service_id"`
	CPU       float64   `json:"cpu"`       // percentage
	Memory    float64   `json:"memory"`    // MB
	MemPct    float64   `json:"mem_pct"`   // percentage
	NetRx     int64     `json:"net_rx"`    // bytes
	NetTx     int64     `json:"net_tx"`    // bytes
	Timestamp time.Time `gorm:"index" json:"timestamp"`
}

// SystemMetric is a global host snapshot
type SystemMetric struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CPU       float64   `json:"cpu"`
	MemTotal  uint64    `json:"mem_total"`
	MemUsed   uint64    `json:"mem_used"`
	DiskTotal uint64    `json:"disk_total"`
	DiskUsed  uint64    `json:"disk_used"`
	LoadAvg   float64   `json:"load_avg"`
	Timestamp time.Time `gorm:"index" json:"timestamp"`
}
