package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sequring/chameleon/auth"
	"github.com/sequring/chameleon/config"
	"github.com/sequring/chameleon/dialer"
	"github.com/sequring/chameleon/metrics" 
	"github.com/sequring/chameleon/proxypool"
	"github.com/sequring/chameleon/utils"
	"github.com/things-go/go-socks5"
)

const AppVersion = "0.1.0"

func main() {
	configPath := flag.String("config", "config.json", "Path to the configuration file")
	testConfig := flag.Bool("t", false, "Test configuration and exit")
	disableTextMetrics := flag.Bool("no-text-metrics", true, "Disable legacy text metrics output to log")

	flag.Parse()

	log.SetFlags(0)

	appCfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration from '%s': %v\n", *configPath, err)
		fmt.Fprintln(os.Stderr, "Configuration test failed.")
		os.Exit(1)
	}

	validationErrors := appCfg.Validate()
	if len(validationErrors) > 0 {
		fmt.Fprintf(os.Stderr, "Configuration validation failed with %d error(s):\n", len(validationErrors))
		errorMessages := make([]string, len(validationErrors))
		for i, e := range validationErrors {
			errorMessages[i] = fmt.Sprintf("  - %s", e.Error())
		}
		fmt.Fprintln(os.Stderr, strings.Join(errorMessages, "\n"))
		os.Exit(1)
	}

	if *testConfig {
		fmt.Println("Configuration test successful.")
		os.Exit(0)
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	utils.PrintBanner(AppVersion)

	config.DefaultProxyCheckInterval, err = time.ParseDuration(config.DefaultProxyCheckIntervalStr)
	if err != nil {
		log.Fatalf("Internal error: Invalid default proxy check interval string '%s': %v", config.DefaultProxyCheckIntervalStr, err)
	}
	config.DefaultProxyCheckTimeout, err = time.ParseDuration(config.DefaultProxyCheckTimeoutStr)
	if err != nil {
		log.Fatalf("Internal error: Invalid default proxy check timeout string '%s': %v", config.DefaultProxyCheckTimeoutStr, err)
	}
	config.MetricsDisplayInterval, err = time.ParseDuration(config.DefaultMetricsIntervalStr)
	if err != nil {
		log.Fatalf("Internal error: Invalid default metrics interval string '%s': %v", config.DefaultMetricsIntervalStr, err)
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

	proxyCheckInterval, _ := time.ParseDuration(appCfg.ProxyCheckInterval)
	proxyCheckTimeout, _ := time.ParseDuration(appCfg.ProxyCheckTimeout)
	currentMetricsInterval, _ := time.ParseDuration(appCfg.MetricsInterval)


	pool := proxypool.New(
		proxyListInternal,
		proxyCheckInterval,
		proxyCheckTimeout,
		appCfg.HealthCheckTarget,
	)


	oldMetricsSvc := &dialer.Metrics{} 
	appDialer := dialer.New(pool, oldMetricsSvc) 
	
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	if appCfg.PrometheusListenAddr != "" {
		promExporter := metrics.NewPrometheusExporter(pool, appCfg.PrometheusListenAddr)
		promExporter.Start() 

		go func() {
			ticker := time.NewTicker(currentMetricsInterval)
			defer ticker.Stop()
			log.Println("Prometheus proxy metrics updater started.")
			for {
				select {
				case <-ticker.C:
					promExporter.UpdateProxyMetrics()
				case <-appCtx.Done():
					log.Println("Prometheus proxy metrics updater stopping...")
					return
				}
			}
		}()
	} else {
		log.Println("Prometheus metrics endpoint is disabled (prometheus_listen_addr not set in config).")
	}


	if !*disableTextMetrics {
		go dialer.PrintMetrics(appCtx, currentMetricsInterval, pool, oldMetricsSvc)
	} else {
		log.Println("Legacy text metrics output is disabled by flag.")
	}


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
			log.Fatalf("SOCKS5 server failed: %v", errVal)
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