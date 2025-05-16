// config/checker.go
package config

import (
	"fmt"
	"net"
	"net/url"
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

	if len(appCfg.Proxies) == 0 {
		errs = append(errs, fmt.Errorf("at least one proxy must be configured in the 'proxies' list"))
	} else {
		for i, p := range appCfg.Proxies {
			if p.Address == "" {
				errs = append(errs, fmt.Errorf("proxy #%d: address cannot be empty", i+1))
			} else {
				_, _, err := net.SplitHostPort(p.Address)
				if err != nil {

					u, urlErr := url.Parse("socks5://" + p.Address)
					if urlErr != nil || u.Host == "" {
						errs = append(errs, fmt.Errorf("proxy #%d: invalid address format '%s': %w (original error: %v)", i+1, p.Address, err, urlErr))
					} else if u.Port() == "" {
                        errs = append(errs, fmt.Errorf("proxy #%d: address '%s' is missing a port", i+1, p.Address))
                    }
				}
			}
			if (p.Username != "" && p.Password == "") || (p.Username == "" && p.Password != "") {
				errs = append(errs, fmt.Errorf("proxy #%d ('%s'): username and password must both be set or both be empty", i+1, p.Address))
			}
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