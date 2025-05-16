// proxypool/pool.go
package proxypool

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/sequring/chameleon/config"
)

// Pool - это определение типа, оно должно быть здесь.
type Pool struct {
	definitionsManager *config.ProxyDefinitionsManager
	proxies            map[string]*ProxyConfig
	mu                 sync.RWMutex
	checkInterval      time.Duration
	timeout            time.Duration
	testURL            string
	wg                 sync.WaitGroup
	overallShutdownCtx    context.Context
	overallShutdownCancel context.CancelFunc
	reloadListenerStop chan struct{}
}

// New создает и инициализирует ProxyPool.
func New(
	definitionsMgr *config.ProxyDefinitionsManager,
	checkInterval, timeout time.Duration,
	testURL string,
) *Pool { // Убедимся, что возвращаемый тип Pool корректен
	overallCtx, overallCancel := context.WithCancel(context.Background())
	pool := &Pool{ // Используем Pool
		definitionsManager: definitionsMgr,
		proxies:            make(map[string]*ProxyConfig),
		checkInterval:      checkInterval,
		timeout:            timeout,
		testURL:            testURL,
		overallShutdownCtx:    overallCtx,
		overallShutdownCancel: overallCancel,
		reloadListenerStop: make(chan struct{}),
	}

	if err := pool.reloadAndReconcileProxies(); err != nil {
		log.Printf("Error during initial proxy load: %v. Pool might be empty or outdated.", err)
	}

	pool.wg.Add(1)
	go pool.listenForReloads()

	return pool
}

// reloadAndReconcileProxies загружает новые определения и обновляет внутреннее состояние пула.
func (p *Pool) reloadAndReconcileProxies() error {
	log.Println("Reloading and reconciling proxies...")
	newDefinitions := p.definitionsManager.GetDefinitions()

	p.mu.Lock()
	defer p.mu.Unlock()

	newProxiesMap := make(map[string]*config.ProxyDefinition)
	for i := range newDefinitions {
		def := &newDefinitions[i]
		newProxiesMap[def.Address] = def
	}

	for addr, existingProxyCfg := range p.proxies {
		if _, existsInNew := newProxiesMap[addr]; !existsInNew {
			log.Printf("Proxy %s removed, stopping its health check.", addr)
			existingProxyCfg.shutdownHealthCheck()
			delete(p.proxies, addr)
		}
	}

	for addr, newDef := range newProxiesMap {
		if existingProxyCfg, exists := p.proxies[addr]; exists {
			needsRestart := false
			if existingProxyCfg.Username != newDef.Username || existingProxyCfg.Password != newDef.Password {
				log.Printf("Proxy %s credentials changed.", addr)
				needsRestart = true
			}
			// Обновляем теги и описание в любом случае
			existingProxyCfg.Mu.Lock()
			existingProxyCfg.Tags = newDef.Tags
			existingProxyCfg.Description = newDef.Description
			existingProxyCfg.Mu.Unlock()

			if needsRestart {
				log.Printf("Restarting health check for proxy %s due to config changes.", addr)
				existingProxyCfg.shutdownHealthCheck()
				p.proxies[addr] = p.createAndStartProxyConfig(newDef)
			}
		} else {
			log.Printf("New proxy %s added, starting its health check.", addr)
			p.proxies[addr] = p.createAndStartProxyConfig(newDef)
		}
	}
	log.Printf("Proxies reconciled. Current active proxy count: %d", len(p.proxies))
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

// listenForReloads слушает канал перезагрузки и вызывает reconcile.
func (p *Pool) listenForReloads() {
	defer p.wg.Done()
	log.Println("ProxyPool: reload listener started.")
	for {
		select {
		case <-p.definitionsManager.ReloadChannel():
			log.Println("ProxyPool: Received reload signal.")
			if err := p.reloadAndReconcileProxies(); err != nil {
				log.Printf("Error during proxy reconciliation: %v", err)
			}
		case <-p.reloadListenerStop:
			log.Println("ProxyPool: reload listener stopping.")
			return
		case <-p.overallShutdownCtx.Done():
			log.Println("ProxyPool: reload listener stopping due to overall shutdown.")
			return
		}
	}
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

// Stop останавливает все health checks и слушателя перезагрузки.
func (p *Pool) Stop() {
	log.Println("ProxyPool stopping all operations...")
	// Сигнал для остановки слушателя перезагрузок (если он еще не остановлен overallShutdown)
	select {
	case <-p.reloadListenerStop: // уже закрыт
	default:
		close(p.reloadListenerStop)
	}

	p.overallShutdownCancel() // Сигнал для остановки всех health check горутин
	
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