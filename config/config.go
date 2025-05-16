package config 

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/sequring/chameleon/auth" 
	"github.com/sequring/chameleon/utils"
)


type ProxyEntry struct { 
	Address  string `json:"address"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}


type App struct { 
	ServerPort         string         `json:"server_port"`
	Proxies            []ProxyEntry   `json:"proxies"`
	ProxyCheckInterval string         `json:"proxy_check_interval"`
	ProxyCheckTimeout  string         `json:"proxy_check_timeout"`
	HealthCheckTarget  string         `json:"health_check_target"`
	MetricsInterval    string         `json:"metrics_interval"`
	Users              []auth.ClientConfig `json:"users"` 
}

var (
	DefaultProxyCheckIntervalStr = "1m"
	DefaultProxyCheckTimeoutStr  = "10s"
	DefaultMetricsIntervalStr    = "30s"
	DefaultServerPortStr         = ":1080"
	DefaultHealthCheckTargetStr  = "www.google.com:443"
)

var (
	DefaultProxyCheckInterval time.Duration
	DefaultProxyCheckTimeout  time.Duration
	MetricsDisplayInterval    time.Duration 
)


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
		username, errUser := utils.GenerateRandomUsername()
		if errUser != nil {
			log.Printf("Error generating random username: %v. Using fallback.", errUser)
			username = "H9NrVNZeUupxfv4G9k"
		}

		password, errPass := utils.GenerateRandomSecurePassword()
		if errPass != nil {
			log.Printf("Error generating random password: %v. Using fallback.", errPass)
			password = "zj9wq5FEH2jj8Ywt7Z" 
		}

		log.Printf("Warning: No users defined in config. Generating a random user.")
		log.Printf("======== DEFAULT USER CREDENTIALS (save these!) ========")
		log.Printf("Username: %s", username)
		log.Printf("Password: %s", password)
		log.Printf("==========================================================")
		appCfg.Users = append(appCfg.Users, auth.ClientConfig{Username: username, Password: password, Allowed: true})
	}

	return &appCfg, nil
}