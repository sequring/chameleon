package proxypool

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type ProxyConfig struct {
	Address      string
	Username     string
	Password     string
	Tags         []string 
	Description  string   
	IsActive     bool
	LastCheck    time.Time
	ResponseTime time.Duration
	SuccessCount uint32
	FailCount    uint32
	Mu           sync.RWMutex

	healthCheckCancelFunc context.CancelFunc 
	hcMu                  sync.Mutex         
}

func (pc *ProxyConfig) MarkActive(responseTime time.Duration) {
	pc.Mu.Lock()
	defer pc.Mu.Unlock()
	pc.IsActive = true
	pc.LastCheck = time.Now()
	pc.ResponseTime = responseTime
}

// String returns a string representation of the ProxyConfig
func (pc *ProxyConfig) String() string {
	pc.Mu.RLock()
	defer pc.Mu.RUnlock()
	return fmt.Sprintf("ProxyConfig{Address: %s, User: %s, Active: %v, LastCheck: %v, ResponseTime: %v}",
		pc.Address, pc.Username, pc.IsActive, pc.LastCheck, pc.ResponseTime)
}

func (pc *ProxyConfig) MarkInactive(checkErr error) { 
	pc.Mu.Lock()
	defer pc.Mu.Unlock()
	pc.IsActive = false
	pc.LastCheck = time.Now()
}

func (pc *ProxyConfig) setHealthCheckCancelFunc(cancel context.CancelFunc) {
	pc.hcMu.Lock()
	defer pc.hcMu.Unlock()
	pc.healthCheckCancelFunc = cancel
}

func (pc *ProxyConfig) shutdownHealthCheck() {
	pc.hcMu.Lock()
	defer pc.hcMu.Unlock()
	if pc.healthCheckCancelFunc != nil {
		pc.healthCheckCancelFunc()
		pc.healthCheckCancelFunc = nil // Предотвращаем повторный вызов
	}
}