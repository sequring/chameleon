package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sequring/chameleon/auth"
	"github.com/sequring/chameleon/config"
	"github.com/sequring/chameleon/dialer"
	"github.com/sequring/chameleon/proxypool"
	"github.com/things-go/go-socks5"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	var err error
	config.DefaultProxyCheckInterval, err = time.ParseDuration(config.DefaultProxyCheckIntervalStr)
	if err != nil {
		log.Fatalf("Invalid default proxy check interval string '%s': %v", config.DefaultProxyCheckIntervalStr, err)
	}
	config.DefaultProxyCheckTimeout, err = time.ParseDuration(config.DefaultProxyCheckTimeoutStr)
	if err != nil {
		log.Fatalf("Invalid default proxy check timeout string '%s': %v", config.DefaultProxyCheckTimeoutStr, err)
	}
	config.MetricsDisplayInterval, err = time.ParseDuration(config.DefaultMetricsIntervalStr)
	if err != nil {
		log.Fatalf("Invalid default metrics interval string '%s': %v", config.DefaultMetricsIntervalStr, err)
	}

	configPath := "config.json"
	appCfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration from %s: %v", configPath, err)
	}

	authService := auth.New()
	for _, u := range appCfg.Users {
		authService.AddClient(u.Username, u.Password, u.Allowed)
		log.Printf("Loaded user: %s (Allowed: %v)", u.Username, u.Allowed)
	}

	proxyListInternal := make([]*proxypool.ProxyConfig, 0, len(appCfg.Proxies))
	for _, pEntry := range appCfg.Proxies {
		proxyListInternal = append(proxyListInternal, &proxypool.ProxyConfig{
			Address:  pEntry.Address,
			Username: pEntry.Username,
			Password: pEntry.Password,
			IsActive: false,
		})
	}

	if len(proxyListInternal) == 0 {
		log.Println("WARNING: No proxies configured in config file.")
	}

	proxyCheckInterval, err := time.ParseDuration(appCfg.ProxyCheckInterval)
	if err != nil {
		log.Printf("Warning: Invalid proxy_check_interval '%s' in config, using default %s. Error: %v", appCfg.ProxyCheckInterval, config.DefaultProxyCheckInterval, err)
		proxyCheckInterval = config.DefaultProxyCheckInterval
	}

	proxyCheckTimeout, err := time.ParseDuration(appCfg.ProxyCheckTimeout)
	if err != nil {
		log.Printf("Warning: Invalid proxy_check_timeout '%s' in config, using default %s. Error: %v", appCfg.ProxyCheckTimeout, config.DefaultProxyCheckTimeout, err)
		proxyCheckTimeout = config.DefaultProxyCheckTimeout
	}

	currentMetricsInterval, err := time.ParseDuration(appCfg.MetricsInterval)
	if err != nil {
		log.Printf("Warning: Invalid metrics_interval '%s' in config, using default %s. Error: %v", appCfg.MetricsInterval, config.MetricsDisplayInterval, err)
		currentMetricsInterval = config.MetricsDisplayInterval
	}

	pool := proxypool.New(
		proxyListInternal,
		proxyCheckInterval,
		proxyCheckTimeout,
		appCfg.HealthCheckTarget,
	)

	metricsSvc := &dialer.Metrics{}
	appDialer := dialer.New(pool, metricsSvc)

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	go dialer.PrintMetrics(appCtx, currentMetricsInterval, pool, metricsSvc)

	socksServerLogger := log.New(log.Writer(), "[SOCKS5_LIB] ", log.LstdFlags|log.Lmicroseconds)

	server := socks5.NewServer(
		socks5.WithDial(appDialer.Dial),
		socks5.WithAuthMethods([]socks5.Authenticator{
			socks5.UserPassAuthenticator{Credentials: authService},
		}),
		socks5.WithLogger(socks5.NewLogger(socksServerLogger)),
	)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	errChan := make(chan error, 1)
	go func() {
		log.Printf("Starting SOCKS5 server on %s", appCfg.ServerPort)
		if errSrv := server.ListenAndServe("tcp", appCfg.ServerPort); errSrv != nil && !errors.Is(errSrv, net.ErrClosed) {
			log.Printf("SOCKS5 server ListenAndServe error: %v", errSrv)
			errChan <- errSrv
		}
		log.Println("SOCKS5 server ListenAndServe goroutine finished.")
		close(errChan)
	}()

	select {
	case errVal, ok := <-errChan:
		if ok && errVal != nil {
			log.Fatalf("Failed to start or run SOCKS5 server: %v", errVal)
		} else if !ok {
			log.Println("SOCKS5 server has stopped (errChan closed).")
		}
	case s := <-sigChan:
		log.Printf("Received signal: %v. Shutting down...", s)
		appCancel()
		pool.Stop()
		log.Println("SOCKS5 server will stop as part of process termination.")
	}
	log.Println("Application finished.")
}