package config 

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)


type ProxyEntry struct {
	Address  string `yaml:"address" json:"address"`
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
	Password string `yaml:"password,omitempty" json:"password,omitempty"`
}


type ServerConfig struct {
	SocksPort string `yaml:"socks_port" json:"socks_port"`
	AdminPort string `yaml:"admin_port" json:"admin_port"`
}

type LoggingConfig struct {
	Directory       string `yaml:"directory" json:"directory"`
	AccessLogFile   string `yaml:"access_log_file" json:"access_log_file"`
	ErrorLogFile    string `yaml:"error_log_file" json:"error_log_file"`
	LogMaxSizeMB    int    `yaml:"log_max_size_mb" json:"log_max_size_mb"`
	LogMaxBackups   int    `yaml:"log_max_backups" json:"log_max_backups"`
	LogMaxAgeDays   int    `yaml:"log_max_age_days" json:"log_max_age_days"`
	LogCompress     bool   `yaml:"log_compress" json:"log_compress"`
}

type ProxiesConfig struct {
	ConfigFilePath      string `yaml:"config_file_path" json:"config_file_path"`
	CheckIntervalSecs   int    `yaml:"check_interval_seconds" json:"check_interval_seconds"`
	CheckTimeoutSecs    int    `yaml:"check_timeout_seconds" json:"check_timeout_seconds"`
	HealthCheckTarget   string `yaml:"health_check_target" json:"health_check_target"`
	// ConfigReloadToken is no longer used and will be removed in a future version
}

type UsersConfig struct {
	ConfigFilePath       string `yaml:"config_file_path" json:"config_file_path"`
	DefaultBehavior      string `yaml:"default_behavior_no_tags" json:"default_behavior_no_tags"`
	DefaultProxyTag      string `yaml:"default_proxy_tag" json:"default_proxy_tag"`
}

type WebhookConfig struct {
	URL            string `yaml:"url" json:"url"`
	PostTimeoutSec int    `yaml:"post_timeout_seconds" json:"post_timeout_seconds"`
}

type PrometheusConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Port    string `yaml:"port" json:"port"`
}

// App represents the application configuration
type App struct {
	Server      ServerConfig      `yaml:"server" json:"server"`
	Logging     LoggingConfig     `yaml:"logging" json:"logging"`
	Proxies     ProxiesConfig     `yaml:"proxies" json:"proxies"`
	Users       UsersConfig       `yaml:"users" json:"users"`
	Webhook     WebhookConfig     `yaml:"webhook,omitempty" json:"webhook,omitempty"`
	Prometheus  PrometheusConfig  `yaml:"prometheus,omitempty" json:"prometheus,omitempty"`
}

// Default configuration values
const (
	DefaultProxyCheckIntervalStr = "1m"
	DefaultProxyCheckTimeoutStr  = "10s"
	DefaultMetricsIntervalStr    = "30s"
	DefaultServerPortStr         = ":1080"
	DefaultHealthCheckTargetStr  = "www.google.com:443"
	DefaultPrometheusListenAddr = ":9091"
	DefaultProxiesFilePath      = "proxies.json"
)

var (
	DefaultProxyCheckInterval time.Duration
	DefaultProxyCheckTimeout  time.Duration
	MetricsDisplayInterval    time.Duration
	// DefaultMetricsInterval is the parsed version of DefaultMetricsIntervalStr
	DefaultMetricsInterval    time.Duration
)

func init() {
	var err error
	DefaultMetricsInterval, err = time.ParseDuration(DefaultMetricsIntervalStr)
	if err != nil {
		panic(fmt.Sprintf("invalid default metrics interval '%s': %v", DefaultMetricsIntervalStr, err))
	}
}


func Load(path string) (*App, error) {
	configFile, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var appCfg App
	err = yaml.Unmarshal(configFile, &appCfg)
	if err != nil {
		return nil, err
	}

	appCfg.applyDefaults()

	return &appCfg, nil
}

// applyDefaults sets default values for any missing configuration fields
func (appCfg *App) applyDefaults() {
	// Server defaults
	if appCfg.Server.SocksPort == "" {
		appCfg.Server.SocksPort = DefaultServerPortStr
	}
	if appCfg.Server.AdminPort == "" {
		appCfg.Server.AdminPort = ":8081"
	}

	// Logging defaults
	if appCfg.Logging.Directory == "" {
		appCfg.Logging.Directory = "logs"
	}
	if appCfg.Logging.AccessLogFile == "" {
		appCfg.Logging.AccessLogFile = "access.log"
	}
	if appCfg.Logging.ErrorLogFile == "" {
		appCfg.Logging.ErrorLogFile = "error.log"
	}
	if appCfg.Logging.LogMaxSizeMB == 0 {
		appCfg.Logging.LogMaxSizeMB = 100
	}
	if appCfg.Logging.LogMaxBackups == 0 {
		appCfg.Logging.LogMaxBackups = 3
	}
	if appCfg.Logging.LogMaxAgeDays == 0 {
		appCfg.Logging.LogMaxAgeDays = 28
	}

	// Proxies defaults
	if appCfg.Proxies.ConfigFilePath == "" {
		appCfg.Proxies.ConfigFilePath = DefaultProxiesFilePath
	}
	if appCfg.Proxies.CheckIntervalSecs == 0 {
		appCfg.Proxies.CheckIntervalSecs = 60
	}
	if appCfg.Proxies.CheckTimeoutSecs == 0 {
		appCfg.Proxies.CheckTimeoutSecs = 10
	}
	if appCfg.Proxies.HealthCheckTarget == "" {
		appCfg.Proxies.HealthCheckTarget = DefaultHealthCheckTargetStr
	}

	// Users defaults
	if appCfg.Users.ConfigFilePath == "" {
		appCfg.Users.ConfigFilePath = "users.json"
	}
	if appCfg.Users.DefaultBehavior == "" {
		appCfg.Users.DefaultBehavior = "allow_default_tag_only"
	}
	if appCfg.Users.DefaultProxyTag == "" {
		appCfg.Users.DefaultProxyTag = "general"
	}

	// Webhook defaults
	if appCfg.Webhook.URL != "" && appCfg.Webhook.PostTimeoutSec == 0 {
		appCfg.Webhook.PostTimeoutSec = 10
	}

	// Prometheus defaults
	if appCfg.Prometheus.Port == "" {
		appCfg.Prometheus.Port = DefaultPrometheusListenAddr
	}
}