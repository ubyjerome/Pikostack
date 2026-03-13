package db

import (
	"time"

	"github.com/google/uuid"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Database struct {
	db *gorm.DB
}

func Init(path string) (*Database, error) {
	gdb, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	// Enable WAL for better concurrency
	gdb.Exec("PRAGMA journal_mode=WAL")
	gdb.Exec("PRAGMA foreign_keys=ON")

	if err := gdb.AutoMigrate(
		&Project{},
		&Service{},
		&ServiceEvent{},
		&Metric{},
		&SystemMetric{},
	); err != nil {
		return nil, err
	}

	return &Database{db: gdb}, nil
}

// ─── Projects ────────────────────────────────────────────────────────────────

func (d *Database) ListProjects() ([]Project, error) {
	var projects []Project
	err := d.db.Preload("Services").Find(&projects).Error
	return projects, err
}

func (d *Database) GetProject(id string) (*Project, error) {
	var p Project
	err := d.db.Preload("Services").First(&p, "id = ?", id).Error
	return &p, err
}

func (d *Database) CreateProject(name, description string) (*Project, error) {
	p := &Project{
		ID:          uuid.New().String(),
		Name:        name,
		Description: description,
	}
	return p, d.db.Create(p).Error
}

func (d *Database) DeleteProject(id string) error {
	return d.db.Transaction(func(tx *gorm.DB) error {
		tx.Where("project_id = ?", id).Delete(&Service{})
		return tx.Delete(&Project{}, "id = ?", id).Error
	})
}

// ─── Services ────────────────────────────────────────────────────────────────

func (d *Database) ListServices() ([]Service, error) {
	var services []Service
	err := d.db.Preload("Project").Find(&services).Error
	return services, err
}

func (d *Database) ListServicesByProject(projectID string) ([]Service, error) {
	var services []Service
	err := d.db.Where("project_id = ?", projectID).Find(&services).Error
	return services, err
}

func (d *Database) GetService(id string) (*Service, error) {
	var s Service
	err := d.db.Preload("Project").First(&s, "id = ?", id).Error
	return &s, err
}

func (d *Database) CreateService(s *Service) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	if s.HealthMethod == "" {
		s.HealthMethod = "GET"
	}
	if s.HealthStatusCode == 0 {
		s.HealthStatusCode = 200
	}
	return d.db.Create(s).Error
}

func (d *Database) UpdateService(s *Service) error {
	return d.db.Save(s).Error
}

func (d *Database) UpdateServiceStatus(id string, status ServiceStatus, pid int) error {
	updates := map[string]interface{}{"status": status}
	if pid > 0 {
		updates["pid"] = pid
	}
	return d.db.Model(&Service{}).Where("id = ?", id).Updates(updates).Error
}

func (d *Database) IncrementRestartCount(id string) error {
	return d.db.Model(&Service{}).Where("id = ?", id).
		UpdateColumn("restart_count", gorm.Expr("restart_count + 1")).Error
}

func (d *Database) SetServiceStarted(id string) error {
	now := time.Now()
	return d.db.Model(&Service{}).Where("id = ?", id).Updates(map[string]interface{}{
		"started_at": &now,
		"status":     StatusRunning,
	}).Error
}

func (d *Database) RecordHealth(id string, ok bool) error {
	now := time.Now()
	return d.db.Model(&Service{}).Where("id = ?", id).Updates(map[string]interface{}{
		"last_health_at": &now,
		"last_health_ok": ok,
	}).Error
}

func (d *Database) DeleteService(id string) error {
	return d.db.Transaction(func(tx *gorm.DB) error {
		tx.Where("service_id = ?", id).Delete(&ServiceEvent{})
		tx.Where("service_id = ?", id).Delete(&Metric{})
		return tx.Delete(&Service{}, "id = ?", id).Error
	})
}

// ─── Events ──────────────────────────────────────────────────────────────────

func (d *Database) RecordEvent(serviceID string, evType EventType, msg string) error {
	e := &ServiceEvent{
		ID:        uuid.New().String(),
		ServiceID: serviceID,
		Type:      evType,
		Message:   msg,
		CreatedAt: time.Now(),
	}
	return d.db.Create(e).Error
}

func (d *Database) ListEvents(serviceID string, limit int) ([]ServiceEvent, error) {
	var events []ServiceEvent
	q := d.db.Order("created_at DESC").Limit(limit)
	if serviceID != "" {
		q = q.Where("service_id = ?", serviceID)
	}
	err := q.Find(&events).Error
	return events, err
}

func (d *Database) PruneEvents(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	return d.db.Where("created_at < ?", cutoff).Delete(&ServiceEvent{}).Error
}

// ─── Metrics ─────────────────────────────────────────────────────────────────

func (d *Database) RecordMetric(m *Metric) error {
	m.Timestamp = time.Now()
	return d.db.Create(m).Error
}

func (d *Database) ListMetrics(serviceID string, since time.Time, limit int) ([]Metric, error) {
	var metrics []Metric
	err := d.db.Where("service_id = ? AND timestamp > ?", serviceID, since).
		Order("timestamp ASC").Limit(limit).Find(&metrics).Error
	return metrics, err
}

func (d *Database) PruneMetrics(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	return d.db.Where("timestamp < ?", cutoff).Delete(&Metric{}).Error
}

func (d *Database) RecordSystemMetric(m *SystemMetric) error {
	m.Timestamp = time.Now()
	return d.db.Create(m).Error
}

func (d *Database) ListSystemMetrics(since time.Time) ([]SystemMetric, error) {
	var metrics []SystemMetric
	err := d.db.Where("timestamp > ?", since).Order("timestamp ASC").Find(&metrics).Error
	return metrics, err
}

// ─── Analytics ───────────────────────────────────────────────────────────────

type ServiceSummary struct {
	Total   int64
	Running int64
	Stopped int64
	Error   int64
}

func (d *Database) GetServiceSummary() (*ServiceSummary, error) {
	s := &ServiceSummary{}
	d.db.Model(&Service{}).Count(&s.Total)
	d.db.Model(&Service{}).Where("status = ?", StatusRunning).Count(&s.Running)
	d.db.Model(&Service{}).Where("status = ?", StatusStopped).Count(&s.Stopped)
	d.db.Model(&Service{}).Where("status = ?", StatusError).Count(&s.Error)
	return s, nil
}

func (d *Database) GetUptimePercent(serviceID string, since time.Time) float64 {
	var total, ok int64
	d.db.Model(&ServiceEvent{}).
		Where("service_id = ? AND created_at > ? AND type = ?", serviceID, since, EventHealthCheck).
		Count(&total)
	d.db.Model(&ServiceEvent{}).
		Where("service_id = ? AND created_at > ? AND type = ? AND message LIKE ?", serviceID, since, EventHealthCheck, "%OK%").
		Count(&ok)
	if total == 0 {
		return 100.0
	}
	return float64(ok) / float64(total) * 100.0
}

// Raw returns the underlying gorm.DB for advanced queries
func (d *Database) Raw() *gorm.DB {
	return d.db
}
