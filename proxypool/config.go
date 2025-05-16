package proxypool

import (
	"sync"
	"time"
)

type ProxyConfig struct {
	Address      string
	Username     string
	Password     string
	IsActive     bool
	LastCheck    time.Time
	ResponseTime time.Duration
	SuccessCount uint32 
	FailCount    uint32 
	Mu           sync.RWMutex 
}

func (pc *ProxyConfig) MarkActive(responseTime time.Duration) {
	pc.Mu.Lock()
	defer pc.Mu.Unlock()
	pc.IsActive = true
	pc.LastCheck = time.Now()
	pc.ResponseTime = responseTime
}

func (pc *ProxyConfig) MarkInactive(checkErr error) { 
	pc.Mu.Lock()
	defer pc.Mu.Unlock()
	pc.IsActive = false
	pc.LastCheck = time.Now()
}