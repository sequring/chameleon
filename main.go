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
	"path/filepath"
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
	// Command line flags
	configPath := flag.String("config", "config.yml", "Path to the configuration file (supports .yml and .json)")
	testConfig := flag.Bool("t", false, "Test configuration and exit")
	enableMetrics := flag.Bool("metrics", true, "Enable legacy text metrics output to log")

	flag.Parse()

	log.SetFlags(0) 
	appCfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading application configuration from '%s': %v\n", *configPath, err)
		fmt.Fprintln(os.Stderr, "Configuration test failed.")
		os.Exit(1)
	}

	validationErrors := appCfg.Validate()
	if len(validationErrors) > 0 {
		fmt.Fprintf(os.Stderr, "Application configuration validation failed with %d error(s):\n", len(validationErrors))
		errorMessages := make([]string, len(validationErrors))
		for i, e := range validationErrors {
			errorMessages[i] = fmt.Sprintf("  - %s", e.Error())
		}
		fmt.Fprintln(os.Stderr, strings.Join(errorMessages, "\n"))
		os.Exit(1)
	}

	// Get absolute path to the proxies file
	abProxiesPath, err := filepath.Abs(appCfg.Proxies.ConfigFilePath)
	if err != nil {
		log.Printf("Warning: Could not get absolute path for proxies file: %v", err)
	}
	proxiesFilePath := abProxiesPath

	log.Printf("Loading proxy definitions from: %s", proxiesFilePath)

	// Check if file exists and is readable
	if _, err := os.Stat(proxiesFilePath); os.IsNotExist(err) {
		log.Printf("Warning: Proxy definitions file '%s' does not exist. Starting with no proxies.", proxiesFilePath)
	} else if err != nil {
		log.Printf("Warning: Cannot access proxy definitions file '%s': %v. Starting with no proxies.", proxiesFilePath, err)
	}

	proxyDefsManager := config.NewProxyDefinitionsManager(proxiesFilePath)
	if err := proxyDefsManager.LoadDefinitions(); err != nil {
		log.Printf("Error loading proxy definitions from '%s': %v", proxiesFilePath, err)
		if *testConfig {
			fmt.Fprintf(os.Stderr, "Proxy definitions file test failed: %v\n", err)
			os.Exit(1)
		}
		log.Println("Proceeding with no proxies. Use the admin API to load proxies later.")
	} else {
		defs := proxyDefsManager.GetDefinitions()
		log.Printf("Successfully loaded %d proxy definitions", len(defs))
		for i, def := range defs {
			log.Printf("  [%d] %s (User: %s, Tags: %v)", i+1, def.Address, def.Username, def.Tags)
		}
	}

	if *testConfig {
		fmt.Println("Configuration test successful (app config and initial proxies file if present).")
		os.Exit(0)
	}

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	utils.PrintBanner(AppVersion)

	// Load users from file
	abUsersPath, err := filepath.Abs(appCfg.Users.ConfigFilePath)
	if err != nil {
		log.Printf("Warning: Could not get absolute path for users file: %v, using relative path", err)
		abUsersPath = appCfg.Users.ConfigFilePath
	}

	users, err := auth.LoadUsersFromFile(abUsersPath)
	if err != nil {
		log.Fatalf("Failed to load users from file: %v", err)
	}
	auth.SetUsers(users)
	log.Printf("Loaded %d users from %s", len(users), abUsersPath)

	proxyCheckInterval := time.Duration(appCfg.Proxies.CheckIntervalSecs) * time.Second
	proxyCheckTimeout := time.Duration(appCfg.Proxies.CheckTimeoutSecs) * time.Second


	pool := proxypool.New(
		proxyDefsManager,
		proxyCheckInterval,
		proxyCheckTimeout,
		appCfg.Proxies.HealthCheckTarget,
	)

	oldMetricsSvc := &dialer.Metrics{}
	appDialer := dialer.New(pool, oldMetricsSvc)

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	// Define metrics interval
	metricsUpdateInterval := 30 * time.Second

	// Start Prometheus metrics server if enabled
	if appCfg.Prometheus.Enabled {
		log.Printf("Initializing Prometheus exporter on port %s", appCfg.Prometheus.Port)
		promExporter := metrics.NewPrometheusExporter(pool, appCfg.Prometheus.Port)
		
		// Start Prometheus server
		go func() {
			log.Println("Starting Prometheus metrics server...")
			if err := promExporter.Start(); err != nil {
				log.Printf("Failed to start Prometheus metrics server: %v", err)
				return
			}
			log.Printf("Prometheus metrics available at http://localhost%s/metrics", appCfg.Prometheus.Port)

			// Start metrics updater
			go func() {
				ticker := time.NewTicker(metricsUpdateInterval)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						promExporter.UpdateProxyMetrics()
					case <-appCtx.Done():
						return
					}
				}
			}()

			// Wait for context cancellation
			<-appCtx.Done()
			
			// Shutdown Prometheus server
			if err := promExporter.Stop(); err != nil {
				log.Printf("Error stopping Prometheus metrics server: %v", err)
			}
		}()
	} else {
		log.Println("Prometheus metrics endpoint is disabled (prometheus.enabled is false)")
	}

	// Start legacy metrics if enabled
	if *enableMetrics {
		go dialer.PrintMetrics(appCtx, metricsUpdateInterval, pool, oldMetricsSvc)
	}

	// Create SOCKS5 server instance
	server := socks5.NewServer(
		socks5.WithDial(appDialer.Dial),
		socks5.WithAuthMethods([]socks5.Authenticator{
			socks5.UserPassAuthenticator{Credentials: auth.GetCredentialStore()},
		}),
	)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	errChan := make(chan error, 1)
	// Start SOCKS5 server
	listenAddr := appCfg.Server.SocksPort
	if listenAddr == "" {
		listenAddr = ":1080"
	}
	
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Failed to start SOCKS5 server: %v", err)
	}
	defer listener.Close()
	
	// Start serving in a goroutine
	go func() {
		if errSrv := server.Serve(listener); errSrv != nil && !errors.Is(errSrv, net.ErrClosed) {
			errChan <- errSrv
		}
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