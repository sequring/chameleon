package proxypool

import (
	"context"
	"crypto/x509"
	"errors"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sequring/chameleon/config"
)

// Pool manages a collection of proxy connections and their health checks
type Pool struct {
	definitionsManager *config.ProxyDefinitionsManager
	proxies           map[string]*ProxyConfig
	mu                sync.RWMutex
	checkInterval     time.Duration
	timeout           time.Duration
	testURL           string
	wg                sync.WaitGroup
	overallShutdownCtx    context.Context
	overallShutdownCancel context.CancelFunc
	tlsCheckConfig    atomic.Value // *TLSCheckConfig
}

// New creates and initializes a new ProxyPool with secure defaults
func New(
	definitionsMgr *config.ProxyDefinitionsManager,
	checkInterval, timeout time.Duration,
	testURL string,
) *Pool {
	overallCtx, overallCancel := context.WithCancel(context.Background())
	pool := &Pool{
		definitionsManager: definitionsMgr,
		proxies:           make(map[string]*ProxyConfig),
		checkInterval:     checkInterval,
		timeout:           timeout,
		testURL:           testURL,
		overallShutdownCtx:    overallCtx,
		overallShutdownCancel: overallCancel,
	}
	pool.tlsCheckConfig.Store(DefaultTLSCheckConfig())

	if err := pool.reloadAndReconcileProxies(); err != nil {
		log.Printf("Error during initial proxy load: %v. Pool might be empty or outdated.", err)
	}

	return pool
}

// reloadAndReconcileProxies загружает новые определения и обновляет внутреннее состояние пула.
// equalStringSlices checks if two string slices contain the same elements regardless of order
func equalStringSlices(a, b []string) bool {
    if len(a) != len(b) {
        return false
    }
    
    items := make(map[string]struct{}, len(a))
    for _, v := range a {
        items[v] = struct{}{}
    }
    
    for _, v := range b {
        if _, exists := items[v]; !exists {
            return false
        }
    }
    
    return true
}

func (p *Pool) reloadAndReconcileProxies() error {
	log.Println("Reloading and reconciling proxies...")
	newDefinitions := p.definitionsManager.GetDefinitions()

	// Log just the count of proxies loaded, not the details
	log.Printf("Loaded %d proxy definitions", len(newDefinitions))
	
	// Debug logging - uncomment only when needed for troubleshooting
	// for i, def := range newDefinitions {
	// 	log.Printf("  [%d] %s (User: %s, Tags: %v, Desc: %s)", 
	// 		i+1, def.Address, def.Username, def.Tags, def.Description)
	// }

	p.mu.Lock()
	defer p.mu.Unlock()

	log.Printf("Current active proxies before reconciliation: %d", len(p.proxies))

	newProxiesMap := make(map[string]*config.ProxyDefinition)
	for i := range newDefinitions {
		def := &newDefinitions[i]
		newProxiesMap[def.Address] = def
	}

	// Remove proxies that are no longer in the config
	for addr, existingProxyCfg := range p.proxies {
		if _, existsInNew := newProxiesMap[addr]; !existsInNew {
			log.Printf("Proxy %s removed from configuration, stopping its health check.", addr)
			existingProxyCfg.shutdownHealthCheck()
			delete(p.proxies, addr)
		}
	}

	// Add or update proxies
	for addr, newDef := range newProxiesMap {
		if existingProxyCfg, exists := p.proxies[addr]; exists {
			needsRestart := false
			if existingProxyCfg.Username != newDef.Username || existingProxyCfg.Password != newDef.Password {
				log.Printf("Proxy %s credentials changed.", addr)
				needsRestart = true
			}
			// Update tags and description
			existingProxyCfg.Mu.Lock()
			tagsChanged := !equalStringSlices(existingProxyCfg.Tags, newDef.Tags)
			descChanged := existingProxyCfg.Description != newDef.Description
			existingProxyCfg.Tags = newDef.Tags
			existingProxyCfg.Description = newDef.Description
			existingProxyCfg.Mu.Unlock()

			if needsRestart || tagsChanged || descChanged {
				log.Printf("Restarting health check for proxy %s due to config changes.", addr)
				existingProxyCfg.shutdownHealthCheck()
				p.proxies[addr] = p.createAndStartProxyConfig(newDef)
			}
		} else {
			log.Printf("New proxy %s added, starting its health check.", addr)
			p.proxies[addr] = p.createAndStartProxyConfig(newDef)
		}
	}

	activeCount := 0
	for _, proxy := range p.proxies {
		proxy.Mu.RLock()
		if proxy.IsActive {
			activeCount++
		}
		proxy.Mu.RUnlock()
	}

	log.Printf("Proxies reconciled. Total proxies: %d, Active proxies: %d", 
		len(p.proxies), activeCount)
	return nil
}

// createAndStartProxyConfig создает ProxyConfig и запускает его health check.
func (p *Pool) createAndStartProxyConfig(def *config.ProxyDefinition) *ProxyConfig {
	proxyCfg := &ProxyConfig{
		Address:     def.Address,
		Username:    def.Username,
		Password:    def.Password,
		Tags:        def.Tags,
		Description: def.Description,
		IsActive:    false,
	}
	p.wg.Add(1)
	go p.healthCheckLoopForProxy(proxyCfg)
	return proxyCfg
}

// healthCheckLoopForProxy - цикл проверки для одного ProxyConfig.
func (p *Pool) healthCheckLoopForProxy(proxyCfg *ProxyConfig) {
	defer p.wg.Done()

	ctx, cancel := context.WithCancel(p.overallShutdownCtx) // Контекст для этой горутины
	defer cancel()
	proxyCfg.setHealthCheckCancelFunc(cancel) // Сохраняем для возможности отмены снаружи

	log.Printf("Health check loop started for proxy %s", proxyCfg.Address)
	p.checkProxy(ctx, proxyCfg) // Первоначальная проверка с новым контекстом

	// Используем p.checkInterval из структуры Pool
	// Если checkInterval очень мал, это может привести к частым проверкам.
	// Убедитесь, что значение checkInterval разумно.
	if p.checkInterval <= 0 {
		log.Printf("Warning: Invalid check_interval (%v) for proxy %s. Health check loop will not run periodically.", p.checkInterval, proxyCfg.Address)
		// Просто ждем отмены, если интервал некорректен
		<-ctx.Done()
		log.Printf("Health check loop for proxy %s stopping (invalid interval)...", proxyCfg.Address)
		return
	}
	ticker := time.NewTicker(p.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.checkProxy(ctx, proxyCfg)
		case <-ctx.Done():
			log.Printf("Health check loop for proxy %s stopping...", proxyCfg.Address)
			return
		}
	}
}


// GetActiveProxy теперь работает с map
func (p *Pool) GetActiveProxy() (*ProxyConfig, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	activeProxies := make([]*ProxyConfig, 0, len(p.proxies))
	for _, proxy := range p.proxies {
		proxy.Mu.RLock()
		isActive := proxy.IsActive
		proxy.Mu.RUnlock()
		if isActive {
			activeProxies = append(activeProxies, proxy)
		}
	}

	if len(activeProxies) == 0 {
		return nil, errors.New("no active proxies available")
	}
	// rand.Seed(time.Now().UnixNano()) // Не нужно для Go 1.20+
	return activeProxies[rand.Intn(len(activeProxies))], nil
}

// ConfigureTLS sets the TLS verification options for proxy health checks.
// skipVerify: If true, disables certificate verification (insecure, not recommended for production).
// rootCAs: Optional pool of root CAs to use for verification. If nil, system defaults are used.
// serverName: Optional server name for SNI and certificate validation.
func (p *Pool) ConfigureTLS(skipVerify bool, rootCAs *x509.CertPool, serverName string) {
	// Create a new config with the provided values
	newConfig := DefaultTLSCheckConfig()
	newConfig.SkipVerify = skipVerify
	newConfig.RootCAs = rootCAs
	newConfig.ServerName = serverName

	// Atomically store the new config
	p.tlsCheckConfig.Store(newConfig)

	if skipVerify {
		log.Println("WARNING: TLS certificate verification is disabled. This makes the connection vulnerable to man-in-the-middle attacks!")
	}
}

// Stop stops all health checks and cleans up resources
func (p *Pool) Stop() {
	log.Println("ProxyPool stopping all operations...")
	p.overallShutdownCancel() // Signal all health check goroutines to stop
	p.wg.Wait()
	log.Println("ProxyPool stopped.")
}

// GetProxiesSnapshot теперь работает с map
func (p *Pool) GetProxiesSnapshot() []*ProxyConfig {
    p.mu.RLock()
    defer p.mu.RUnlock()
    snapshot := make([]*ProxyConfig, 0, len(p.proxies))
    for _, proxy := range p.proxies {
        snapshot = append(snapshot, proxy)
    }
    return snapshot
}