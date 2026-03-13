package config

import (
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Monitor  MonitorConfig  `mapstructure:"monitor"`
	Auth     AuthConfig     `mapstructure:"auth"`
}

type ServerConfig struct {
	Host   string `mapstructure:"host"`
	Port   int    `mapstructure:"port"`
	Secret string `mapstructure:"secret"`
}

type DatabaseConfig struct {
	Path string `mapstructure:"path"`
}

type MonitorConfig struct {
	// How often to poll service health
	Interval time.Duration `mapstructure:"interval"`
	// Grace period before triggering auto-restart
	GracePeriod time.Duration `mapstructure:"grace_period"`
	// Max consecutive restarts before giving up
	MaxRestarts int `mapstructure:"max_restarts"`
	// Retain metrics for N days
	MetricsRetentionDays int `mapstructure:"metrics_retention_days"`
	// Retain events for N days
	EventsRetentionDays int `mapstructure:"events_retention_days"`
}

type AuthConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

func Load() *Config {
	setDefaults()
	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		panic("config unmarshal: " + err.Error())
	}
	return cfg
}

func setDefaults() {
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 7331)
	viper.SetDefault("server.secret", "change-me-in-production")

	viper.SetDefault("database.path", "./pikostack.db")

	viper.SetDefault("monitor.interval", 15*time.Second)
	viper.SetDefault("monitor.grace_period", 30*time.Second)
	viper.SetDefault("monitor.max_restarts", 5)
	viper.SetDefault("monitor.metrics_retention_days", 7)
	viper.SetDefault("monitor.events_retention_days", 30)

	viper.SetDefault("auth.enabled", false)
	viper.SetDefault("auth.username", "admin")
	viper.SetDefault("auth.password", "pikostack")
}
