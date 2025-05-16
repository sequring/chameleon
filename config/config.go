package config // Имя пакета соответствует имени каталога

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/sequring/chameleon/auth" // Импорт нашего пакета auth
)

// Структура для отдельного прокси в файле конфигурации
type ProxyEntry struct { // Переименовано, чтобы не конфликтовать с ProxyConfig из proxypool
	Address  string `json:"address"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// Основная структура конфигурации приложения
type App struct { // Переименовано из AppConfig для краткости и избежания AppConfigConfig
	ServerPort         string         `json:"server_port"`
	Proxies            []ProxyEntry   `json:"proxies"`
	ProxyCheckInterval string         `json:"proxy_check_interval"`
	ProxyCheckTimeout  string         `json:"proxy_check_timeout"`
	HealthCheckTarget  string         `json:"health_check_target"`
	MetricsInterval    string         `json:"metrics_interval"`
	Users              []auth.ClientConfig `json:"users"` // Используем ClientConfig из пакета auth
}

// Глобальные переменные для значений по умолчанию из строк
var (
	DefaultProxyCheckIntervalStr = "1m"
	DefaultProxyCheckTimeoutStr  = "10s"
	DefaultMetricsIntervalStr    = "30s"
	DefaultServerPortStr         = ":1080"
	DefaultHealthCheckTargetStr  = "www.google.com:443"
)

// Глобальные переменные для Duration, будут инициализированы в main.go
// Они экспортируемы (начинаются с большой буквы), чтобы main мог их установить.
var (
	DefaultProxyCheckInterval time.Duration
	DefaultProxyCheckTimeout  time.Duration
	MetricsDisplayInterval    time.Duration // Это будет использоваться как значение по умолчанию для метрик
)


// Load загружает конфигурацию из файла
func Load(path string) (*App, error) {
	configFile, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var appCfg App
	err = json.Unmarshal(configFile, &appCfg)
	if err != nil {
		return nil, err
	}

	// Установка значений по умолчанию, если они не указаны в конфиге
	if appCfg.ServerPort == "" {
		appCfg.ServerPort = DefaultServerPortStr
	}
	if appCfg.ProxyCheckInterval == "" {
		appCfg.ProxyCheckInterval = DefaultProxyCheckIntervalStr
	}
	if appCfg.ProxyCheckTimeout == "" {
		appCfg.ProxyCheckTimeout = DefaultProxyCheckTimeoutStr
	}
	if appCfg.HealthCheckTarget == "" {
		appCfg.HealthCheckTarget = DefaultHealthCheckTargetStr
	}
	if appCfg.MetricsInterval == "" {
		appCfg.MetricsInterval = DefaultMetricsIntervalStr
	}
	if len(appCfg.Users) == 0 {
		log.Println("Warning: No users defined in config. Adding default user 'user:pass'.")
		appCfg.Users = append(appCfg.Users, auth.ClientConfig{Username: "user", Password: "pass", Allowed: true})
	}

	return &appCfg, nil
}