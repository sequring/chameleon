// config/checker.go
package config

import (
	"fmt"
	"net"
	"time"
)

type UserEntry struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Allowed  bool   `json:"allowed"`
}

func (appCfg *App) Validate() []error {
	var errs []error

	if appCfg.ServerPort == "" {
		errs = append(errs, fmt.Errorf("server_port must be set"))
	} else if _, _, err := net.SplitHostPort(appCfg.ServerPort); err != nil && !isValidPort(appCfg.ServerPort) {
		errs = append(errs, fmt.Errorf("invalid server_port format '%s': %w. Expected host:port or :port", appCfg.ServerPort, err))
	}


	if _, err := time.ParseDuration(appCfg.ProxyCheckInterval); err != nil {
		errs = append(errs, fmt.Errorf("invalid proxy_check_interval '%s': %w", appCfg.ProxyCheckInterval, err))
	}
	if _, err := time.ParseDuration(appCfg.ProxyCheckTimeout); err != nil {
		errs = append(errs, fmt.Errorf("invalid proxy_check_timeout '%s': %w", appCfg.ProxyCheckTimeout, err))
	}
	if _, err := time.ParseDuration(appCfg.MetricsInterval); err != nil {
		errs = append(errs, fmt.Errorf("invalid metrics_interval '%s': %w", appCfg.MetricsInterval, err))
	}

	if appCfg.HealthCheckTarget == "" {
		errs = append(errs, fmt.Errorf("health_check_target must be set"))
	} else {
		_, _, err := net.SplitHostPort(appCfg.HealthCheckTarget)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid health_check_target format '%s': %w. Expected host:port", appCfg.HealthCheckTarget, err))
		}
	}

	if appCfg.PrometheusListenAddr != "" {
		_, _, err := net.SplitHostPort(appCfg.PrometheusListenAddr)
		isJustPort := false
		if err != nil {
			if len(appCfg.PrometheusListenAddr) > 0 && appCfg.PrometheusListenAddr[0] == ':' {
				_, portErr := net.LookupPort("tcp", appCfg.PrometheusListenAddr[1:])
				if portErr == nil {
					isJustPort = true
				}
			}
	}
	if err != nil && !isJustPort {
			errs = append(errs, fmt.Errorf("invalid prometheus_listen_addr format '%s': %w. Expected host:port or :port", appCfg.PrometheusListenAddr, err))
		}
	}

if appCfg.ProxiesFilePath == "" {
		errs = append(errs, fmt.Errorf("proxies_file_path must be set"))
	}

	if appCfg.ProxyReloadListenAddr != "" && appCfg.ProxyReloadToken == "" {
		errs = append(errs, fmt.Errorf("proxy_reload_token must be set if proxy_reload_listen_addr is configured"))
	}
	
	if appCfg.ProxyReloadListenAddr != "" {
		_, _, err := net.SplitHostPort(appCfg.ProxyReloadListenAddr)
		isJustPort := false
		if err != nil {
			if len(appCfg.ProxyReloadListenAddr) > 0 && appCfg.ProxyReloadListenAddr[0] == ':' {
				_, portErr := net.LookupPort("tcp", appCfg.ProxyReloadListenAddr[1:])
				if portErr == nil {
					isJustPort = true
				}
			}
		}
		if err != nil && !isJustPort {
			errs = append(errs, fmt.Errorf("invalid proxy_reload_listen_addr format '%s': %w. Expected host:port or :port", appCfg.ProxyReloadListenAddr, err))
		}
	}

	if len(appCfg.Users) > 0 {
		for i, u := range appCfg.Users {
			if u.Username == "" {
				errs = append(errs, fmt.Errorf("user #%d: username cannot be empty (this should not happen with auto-generation)", i+1))
			}
			if u.Password == "" {
				errs = append(errs, fmt.Errorf("user #%d ('%s'): password cannot be empty (this should not happen with auto-generation)", i+1, u.Username))
			}
		}
	} else {
		errs = append(errs, fmt.Errorf("internal error: user list is unexpectedly empty after loading configuration"))
	}


	return errs
}

func isValidPort(s string) bool {
    if len(s) > 0 && s[0] == ':' {
        _, err := net.LookupPort("tcp", s[1:])
        return err == nil
    }
    return false
}