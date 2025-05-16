package dialer

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/sequring/chameleon/proxypool" 
)

type Metrics struct {
	TotalRequests uint64
	TotalSuccess  uint64
	TotalFailed   uint64
}

func PrintMetrics(ctx context.Context, interval time.Duration, pPool *proxypool.Pool, m *Metrics) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Println("Metrics printer started")
	for {
		select {
		case <-ticker.C:
			total := atomic.LoadUint64(&m.TotalRequests)
			success := atomic.LoadUint64(&m.TotalSuccess)
			failed := atomic.LoadUint64(&m.TotalFailed)
			var successRate float64
			if total > 0 {
				successRate = float64(success) / float64(total) * 100
			}
			log.Printf("Global Metrics: TotalReq=%d, Success=%d (%.1f%%), Failed=%d", total, success, successRate, failed)

			proxiesSnapshot := pPool.GetProxiesSnapshot() 

			for _, proxy := range proxiesSnapshot {
				proxy.Mu.RLock()
				lastCheckStr := "Never"
				if !proxy.LastCheck.IsZero() {
					lastCheckStr = proxy.LastCheck.Format(time.RFC3339Nano)
				}
				log.Printf("Proxy %s: Active=%v, RespTime=%v, LastCheck=%s, Success=%d, Fail=%d",
					proxy.Address, proxy.IsActive, proxy.ResponseTime, lastCheckStr,
					atomic.LoadUint32(&proxy.SuccessCount), atomic.LoadUint32(&proxy.FailCount))
				proxy.Mu.RUnlock()
			}
		case <-ctx.Done():
			log.Println("Metrics printer stopping...")
			return
		}
	}
}