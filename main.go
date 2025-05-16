package main

import (
	"context"
	"crypto/tls"
	"encoding/json" // Убедимся, что этот импорт есть
	"errors"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/things-go/go-socks5"
	px "golang.org/x/net/proxy"
)

// Структура для отдельного прокси в файле конфигурации
type ConfigProxyEntry struct {
	Address  string `json:"address"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// Основная структура конфигурации приложения
type AppConfig struct {
	ServerPort         string             `json:"server_port"`
	Proxies            []ConfigProxyEntry `json:"proxies"`
	ProxyCheckInterval string             `json:"proxy_check_interval"`
	ProxyCheckTimeout  string             `json:"proxy_check_timeout"`
	HealthCheckTarget  string             `json:"health_check_target"`
	MetricsInterval    string             `json:"metrics_interval"`
	Users              []ClientConfig     `json:"users"`
}

type ClientConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Allowed  bool   `json:"allowed"`
}

type ProxyConfig struct {
	Address      string
	Username     string
	Password     string
	IsActive     bool
	LastCheck    time.Time
	ResponseTime time.Duration
	SuccessCount uint32
	FailCount    uint32
	mu           sync.RWMutex
}

type ProxyPool struct {
	proxies        []*ProxyConfig
	mu             sync.RWMutex
	checkInterval  time.Duration
	timeout        time.Duration
	testURL        string
	wg             sync.WaitGroup
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
}

type ProxyMetrics struct {
	TotalRequests uint64
	TotalSuccess  uint64
	TotalFailed   uint64
}

type ProxyDialer struct {
	proxyPool *ProxyPool
	metrics   *ProxyMetrics
}

type MultiAuth struct {
	clients map[string]ClientConfig
	mu      sync.RWMutex
}

var _ socks5.CredentialStore = (*MultiAuth)(nil)

func NewMultiAuth() *MultiAuth {
	return &MultiAuth{
		clients: make(map[string]ClientConfig),
	}
}

func (a *MultiAuth) AddClient(username, password string, allowed bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.clients[username] = ClientConfig{Username: username, Password: password, Allowed: allowed}
}

func (a *MultiAuth) Valid(username, password, addr string) bool {
	a.mu.RLock()
	client, ok := a.clients[username]
	a.mu.RUnlock()

	if !ok {
		log.Printf("Auth attempt: client not found '%s'", username)
		return false
	}
	if !client.Allowed {
		log.Printf("Auth attempt: client access denied for '%s'", username)
		return false
	}
	if client.Password != password {
		log.Printf("Auth attempt: invalid password for '%s'", username)
		return false
	}
	log.Printf("Auth success for client '%s'", username)
	return true
}

func NewProxyPool(proxies []*ProxyConfig, checkInterval, timeout time.Duration, testURL string) *ProxyPool {
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	pool := &ProxyPool{
		proxies:        proxies,
		checkInterval:  checkInterval,
		timeout:        timeout,
		testURL:        testURL,
		shutdownCtx:    shutdownCtx,
		shutdownCancel: shutdownCancel,
	}
	pool.wg.Add(1)
	go pool.healthCheckLoop()
	return pool
}

func (p *ProxyPool) healthCheckLoop() {
	defer p.wg.Done()
	log.Println("Health check loop started")
	p.checkAllProxies()

	ticker := time.NewTicker(p.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.checkAllProxies()
		case <-p.shutdownCtx.Done():
			log.Println("Health check loop stopping...")
			return
		}
	}
}

func (p *ProxyPool) checkAllProxies() {
	p.mu.RLock()
	proxiesToCheck := make([]*ProxyConfig, len(p.proxies))
	copy(proxiesToCheck, p.proxies)
	p.mu.RUnlock()

	log.Printf("Starting health check for %d proxies...", len(proxiesToCheck))
	var checkWg sync.WaitGroup
	for _, proxyCfg := range proxiesToCheck {
		checkWg.Add(1)
		p.wg.Add(1)
		go func(pc *ProxyConfig) {
			defer checkWg.Done()
			defer p.wg.Done()
			p.checkProxy(p.shutdownCtx, pc)
		}(proxyCfg)
	}
	checkWg.Wait()
	log.Println("Health check for all proxies finished.")
}

func (p *ProxyPool) checkProxy(ctx context.Context, proxyCfg *ProxyConfig) {
	start := time.Now()
	checkCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	var auth *px.Auth
	if proxyCfg.Username != "" {
		auth = &px.Auth{User: proxyCfg.Username, Password: proxyCfg.Password}
	}

	dialer, err := px.SOCKS5("tcp", proxyCfg.Address, auth, px.Direct)
	if err != nil {
		log.Printf("Proxy %s: failed to create SOCKS5 dialer: %v", proxyCfg.Address, err)
		proxyCfg.markInactive(err)
		return
	}

	targetHost := p.testURL
	hostNameForTLS := targetHost
	if strings.Contains(targetHost, ":") {
		hostNameForTLS, _, err = net.SplitHostPort(targetHost)
		if err != nil {
			log.Printf("Proxy %s: invalid testURL format '%s' for SplitHostPort: %v", proxyCfg.Address, targetHost, err)
			proxyCfg.markInactive(err)
			return
		}
	}

	conn, err := dialContext(checkCtx, dialer, "tcp", targetHost)

	if err != nil {
		select {
		case <-checkCtx.Done():
			log.Printf("Proxy %s check timed out or cancelled: %v (underlying error: %v)", proxyCfg.Address, checkCtx.Err(), err)
		default:
			log.Printf("Proxy %s: failed to dial test URL '%s': %v", proxyCfg.Address, targetHost, err)
		}
		proxyCfg.markInactive(err)
		return
	}
	defer conn.Close()

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: hostNameForTLS,
		MinVersion: tls.VersionTLS12,
	})

	if dl, ok := checkCtx.Deadline(); ok {
		tlsConn.SetDeadline(dl)
	}

	if err := tlsConn.HandshakeContext(checkCtx); err != nil {
		select {
		case <-checkCtx.Done():
			log.Printf("Proxy %s: TLS handshake to '%s' timed out or cancelled: %v (underlying error: %v)", proxyCfg.Address, targetHost, checkCtx.Err(), err)
		default:
			log.Printf("Proxy %s: TLS handshake to '%s' failed: %v", proxyCfg.Address, targetHost, err)
		}
		proxyCfg.markInactive(err)
		return
	}

	proxyCfg.markActive(time.Since(start))
	log.Printf("Proxy %s is active, response time: %v", proxyCfg.Address, proxyCfg.ResponseTime)
}

func dialContext(ctx context.Context, dialer px.Dialer, network, address string) (net.Conn, error) {
	var conn net.Conn
	var err error

	done := make(chan struct{})
	go func() {
		conn, err = dialer.Dial(network, address)
		close(done)
	}()

	select {
	case <-ctx.Done():
		if conn != nil {
			conn.Close()
		}
		return nil, ctx.Err()
	case <-done:
		return conn, err
	}
}

func (pc *ProxyConfig) markActive(responseTime time.Duration) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.IsActive = true
	pc.LastCheck = time.Now()
	pc.ResponseTime = responseTime
}

func (pc *ProxyConfig) markInactive(checkErr error) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.IsActive = false
	pc.LastCheck = time.Now()
}

func (p *ProxyPool) GetActiveProxy() (*ProxyConfig, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	activeProxies := make([]*ProxyConfig, 0, len(p.proxies))
	for _, proxy := range p.proxies {
		proxy.mu.RLock()
		if proxy.IsActive {
			activeProxies = append(activeProxies, proxy)
		}
		proxy.mu.RUnlock()
	}

	if len(activeProxies) == 0 {
		return nil, errors.New("no active proxies available")
	}
	return activeProxies[rand.Intn(len(activeProxies))], nil
}

func (p *ProxyPool) Stop() {
	log.Println("ProxyPool stopping...")
	p.shutdownCancel()
	p.wg.Wait()
	log.Println("ProxyPool stopped.")
}

func (d *ProxyDialer) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	atomic.AddUint64(&d.metrics.TotalRequests, 1)

	proxyCfg, err := d.proxyPool.GetActiveProxy()
	if err != nil {
		atomic.AddUint64(&d.metrics.TotalFailed, 1)
		log.Printf("Failed to get active proxy: %v", err)
		return nil, err
	}

	var auth *px.Auth
	if proxyCfg.Username != "" {
		auth = &px.Auth{User: proxyCfg.Username, Password: proxyCfg.Password}
	}

	upstreamDialer, err := px.SOCKS5(network, proxyCfg.Address, auth, px.Direct)
	if err != nil {
		atomic.AddUint64(&d.metrics.TotalFailed, 1)
		atomic.AddUint32(&proxyCfg.FailCount, 1)
		log.Printf("Proxy %s: failed to create SOCKS5 dialer for client request to %s: %v", proxyCfg.Address, addr, err)
		return nil, err
	}

	dialOpTimeout := 15 * time.Second

	dialProxyCtx, dialProxyCancel := context.WithTimeout(ctx, dialOpTimeout)
	defer dialProxyCancel()

	connCh := make(chan net.Conn, 1)
	errCh := make(chan error, 1)

	go func() {
		c, e := dialContext(dialProxyCtx, upstreamDialer, network, addr)
		if e != nil {
			errCh <- e
			return
		}
		connCh <- c
	}()

	select {
	case c := <-connCh:
		atomic.AddUint64(&d.metrics.TotalSuccess, 1)
		atomic.AddUint32(&proxyCfg.SuccessCount, 1)
		log.Printf("Successfully connected to %s via proxy %s", addr, proxyCfg.Address)
		return c, nil
	case e := <-errCh:
		atomic.AddUint64(&d.metrics.TotalFailed, 1)
		atomic.AddUint32(&proxyCfg.FailCount, 1)
		log.Printf("Failed to connect to %s via proxy %s: %v (dialProxyCtx.Err: %v, original_ctx.Err: %v)", addr, proxyCfg.Address, e, dialProxyCtx.Err(), ctx.Err())
		return nil, e
	case <-dialProxyCtx.Done():
		atomic.AddUint64(&d.metrics.TotalFailed, 1)
		atomic.AddUint32(&proxyCfg.FailCount, 1)
		err := errors.New("dialing " + addr + " via proxy " + proxyCfg.Address + " timed out or was cancelled: " + dialProxyCtx.Err().Error())
		log.Print(err.Error())
		return nil, err
	}
}

func (d *ProxyDialer) printMetrics(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Println("Metrics printer started")
	for {
		select {
		case <-ticker.C:
			total := atomic.LoadUint64(&d.metrics.TotalRequests)
			success := atomic.LoadUint64(&d.metrics.TotalSuccess)
			failed := atomic.LoadUint64(&d.metrics.TotalFailed)
			var successRate float64
			if total > 0 {
				successRate = float64(success) / float64(total) * 100
			}
			log.Printf("Global Metrics: TotalReq=%d, Success=%d (%.1f%%), Failed=%d", total, success, successRate, failed)

			d.proxyPool.mu.RLock()
			proxiesSnapshot := make([]*ProxyConfig, len(d.proxyPool.proxies))
			copy(proxiesSnapshot, d.proxyPool.proxies)
			d.proxyPool.mu.RUnlock()

			for _, proxy := range proxiesSnapshot {
				proxy.mu.RLock()
				log.Printf("Proxy %s: Active=%v, RespTime=%v, LastCheck=%s, Success=%d, Fail=%d",
					proxy.Address, proxy.IsActive, proxy.ResponseTime, proxy.LastCheck.Format(time.RFC3339Nano),
					atomic.LoadUint32(&proxy.SuccessCount), atomic.LoadUint32(&proxy.FailCount))
				proxy.mu.RUnlock()
			}
		case <-ctx.Done():
			log.Println("Metrics printer stopping...")
			return
		}
	}
}

// Функция для загрузки конфигурации
func loadConfig(path string) (*AppConfig, error) { // AppConfig здесь используется
	configFile, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config AppConfig // AppConfig здесь используется
	err = json.Unmarshal(configFile, &config) // json здесь используется
	if err != nil {
		return nil, err
	}

	// Установка значений по умолчанию, если они не указаны в конфиге
	if config.ServerPort == "" {
		config.ServerPort = defaultServerPort
	}
	if config.ProxyCheckInterval == "" {
		config.ProxyCheckInterval = defaultProxyCheckIntervalStr // Используем строковые значения по умолчанию
	}
	if config.ProxyCheckTimeout == "" {
		config.ProxyCheckTimeout = defaultProxyCheckTimeoutStr // Используем строковые значения по умолчанию
	}
	if config.HealthCheckTarget == "" {
		config.HealthCheckTarget = defaultHealthCheckTarget
	}
	if config.MetricsInterval == "" {
		config.MetricsInterval = defaultMetricsIntervalStr // Используем строковые значения по умолчанию
	}
	if len(config.Users) == 0 {
		log.Println("Warning: No users defined in config. Adding default user 'user:pass'.")
		config.Users = append(config.Users, ClientConfig{Username: "user", Password: "pass", Allowed: true})
	}

	return &config, nil
}


const (
	defaultProxyCheckIntervalStr = "1m"
	defaultProxyCheckTimeoutStr  = "10s"
	defaultMetricsIntervalStr    = "30s"
	defaultServerPort            = ":1080"
	defaultHealthCheckTarget     = "www.google.com:443"
)

var (
	defaultProxyCheckInterval time.Duration
	defaultProxyCheckTimeout  time.Duration
	metricsDisplayInterval    time.Duration
)


func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Инициализация duration-переменных по умолчанию
	var err error
	defaultProxyCheckInterval, err = time.ParseDuration(defaultProxyCheckIntervalStr)
	if err != nil {
		log.Fatalf("Invalid default proxy check interval: %v", err)
	}
	defaultProxyCheckTimeout, err = time.ParseDuration(defaultProxyCheckTimeoutStr)
	if err != nil {
		log.Fatalf("Invalid default proxy check timeout: %v", err)
	}
	metricsDisplayInterval, err = time.ParseDuration(defaultMetricsIntervalStr)
	if err != nil {
		log.Fatalf("Invalid default metrics interval: %v", err)
	}


	configPath := "config.json"
	appCfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration from %s: %v", configPath, err)
	}

	auth := NewMultiAuth()
	for _, u := range appCfg.Users {
		auth.AddClient(u.Username, u.Password, u.Allowed)
		log.Printf("Loaded user: %s (Allowed: %v)", u.Username, u.Allowed)
	}

	proxyListInternal := make([]*ProxyConfig, 0, len(appCfg.Proxies))
	for _, pEntry := range appCfg.Proxies {
		proxyListInternal = append(proxyListInternal, &ProxyConfig{
			Address:  pEntry.Address,
			Username: pEntry.Username,
			Password: pEntry.Password,
			IsActive: false,
		})
	}

	if len(proxyListInternal) == 0 {
		log.Println("WARNING: No proxies configured.")
	}

	proxyCheckInterval, err := time.ParseDuration(appCfg.ProxyCheckInterval)
	if err != nil {
		log.Printf("Warning: Invalid proxy_check_interval '%s', using default %s. Error: %v", appCfg.ProxyCheckInterval, defaultProxyCheckInterval, err)
		proxyCheckInterval = defaultProxyCheckInterval
	}

	proxyCheckTimeout, err := time.ParseDuration(appCfg.ProxyCheckTimeout)
	if err != nil {
		log.Printf("Warning: Invalid proxy_check_timeout '%s', using default %s. Error: %v", appCfg.ProxyCheckTimeout, defaultProxyCheckTimeout, err)
		proxyCheckTimeout = defaultProxyCheckTimeout
	}

	currentMetricsInterval, err := time.ParseDuration(appCfg.MetricsInterval)
	if err != nil {
		log.Printf("Warning: Invalid metrics_interval '%s', using default %s. Error: %v", appCfg.MetricsInterval, metricsDisplayInterval, err)
		currentMetricsInterval = metricsDisplayInterval
	}

	proxyPool := NewProxyPool(
		proxyListInternal,
		proxyCheckInterval,
		proxyCheckTimeout,
		appCfg.HealthCheckTarget,
	)

	metrics := &ProxyMetrics{}
	dialer := &ProxyDialer{
		proxyPool: proxyPool,
		metrics:   metrics,
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	go dialer.printMetrics(appCtx, currentMetricsInterval)

	socksServerLogger := log.New(log.Writer(), "[SOCKS5_LIB] ", log.LstdFlags|log.Lmicroseconds)

	server := socks5.NewServer(
		socks5.WithDial(dialer.Dial),
		socks5.WithAuthMethods([]socks5.Authenticator{
			socks5.UserPassAuthenticator{Credentials: auth},
		}),
		socks5.WithLogger(socks5.NewLogger(socksServerLogger)),
	)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	errChan := make(chan error, 1)
	go func() {
		log.Printf("Starting SOCKS5 server on %s", appCfg.ServerPort)
		if err := server.ListenAndServe("tcp", appCfg.ServerPort); err != nil && !errors.Is(err, net.ErrClosed) {
			log.Printf("SOCKS5 server ListenAndServe error: %v", err)
			errChan <- err
		}
		log.Println("SOCKS5 server ListenAndServe goroutine finished.")
		close(errChan)
	}()

	select {
	case err, ok := <-errChan:
		if ok && err != nil {
			log.Fatalf("Failed to start or run SOCKS5 server: %v", err)
		} else if !ok {
			log.Println("SOCKS5 server has stopped (errChan closed).")
		}
	case s := <-sigChan:
		log.Printf("Received signal: %v. Shutting down...", s)
		appCancel()
		proxyPool.Stop()
		log.Println("SOCKS5 server will stop as part of process termination.")
	}
	log.Println("Application finished.")
}