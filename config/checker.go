package config

import (
	"fmt"
	"net"
	"strconv"
)

func (appCfg *App) Validate() []error {
	var errs []error

	// Validate server configuration
	if appCfg.Server.SocksPort == "" {
		errs = append(errs, fmt.Errorf("server.socks_port must be set"))
	} else if _, _, err := net.SplitHostPort(appCfg.Server.SocksPort); err != nil && !isValidPort(appCfg.Server.SocksPort) {
		errs = append(errs, fmt.Errorf("invalid server.socks_port format '%s': %w. Expected 'port', ':port', or 'host:port'", appCfg.Server.SocksPort, err))
	}

	// Validate proxy configuration
	if appCfg.Proxies.ConfigFilePath == "" {
		errs = append(errs, fmt.Errorf("proxies.config_file_path must be set"))
	}

	// Validate health check target
	if appCfg.Proxies.HealthCheckTarget == "" {
		errs = append(errs, fmt.Errorf("proxies.health_check_target must be set"))
	} else if _, _, err := net.SplitHostPort(appCfg.Proxies.HealthCheckTarget); err != nil {
		errs = append(errs, fmt.Errorf("invalid proxies.health_check_target format '%s': %w. Expected host:port", appCfg.Proxies.HealthCheckTarget, err))
	}

	// Validate admin port if set
	if appCfg.Server.AdminPort != "" {
		_, _, err := net.SplitHostPort(appCfg.Server.AdminPort)
		isJustPort := false
		if err != nil {
if len(appCfg.Server.AdminPort) > 0 && appCfg.Server.AdminPort[0] == ':' &&
   isValidPort(appCfg.Server.AdminPort[1:]) {
    isJustPort = true
}
		}
		if err != nil && !isJustPort {
			errs = append(errs, fmt.Errorf("invalid server.admin_port format '%s': %w. Expected host:port or :port", appCfg.Server.AdminPort, err))
		}
	}

	// Validate users configuration
	if appCfg.Users.ConfigFilePath == "" {
		errs = append(errs, fmt.Errorf("users.config_file_path must be set"))
	}

	// Validate webhook URL if set
	if appCfg.Webhook.URL != "" {
		if appCfg.Webhook.PostTimeoutSec <= 0 {
			errs = append(errs, fmt.Errorf("webhook.post_timeout_seconds must be greater than 0"))
		}
	}

	return errs
}

// Helper function to check if a string is a valid port
func isValidPort(portStr string) bool {
	if len(portStr) == 0 {
		return false
	}
	// Remove leading colon if present
	if portStr[0] == ':' {
		portStr = portStr[1:]
	}
	// Parse the port number
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return false
	}
	// Check if port is in valid range (1-65535)
	return port > 0 && port <= 65535
}