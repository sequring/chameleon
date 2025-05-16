package proxypool

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"sync"
	"time"
)

type Pool struct {
	proxies        []*ProxyConfig 
	mu             sync.RWMutex
	checkInterval  time.Duration
	timeout        time.Duration
	testURL        string
	wg             sync.WaitGroup
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
}

func New(proxies []*ProxyConfig, checkInterval, timeout time.Duration, testURL string) *Pool {
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	pool := &Pool{
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

func (p *Pool) healthCheckLoop() {
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
	return activeProxies[rand.Intn(len(activeProxies))], nil
}

func (p *Pool) Stop() {
	log.Println("ProxyPool stopping...")
	p.shutdownCancel()
	p.wg.Wait()
	log.Println("ProxyPool stopped.")
}

func (p *Pool) GetProxiesSnapshot() []*ProxyConfig {
    p.mu.RLock()
    defer p.mu.RUnlock()
    snapshot := make([]*ProxyConfig, len(p.proxies))
    copy(snapshot, p.proxies)
    return snapshot
}