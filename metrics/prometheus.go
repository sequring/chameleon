// metrics/prometheus.go
package metrics

import (
	"log"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sequring/chameleon/proxypool" 
)

const namespace = "chameleon" 

var (
	SocksRequestsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "socks",
		Name:      "requests_total",
		Help:      "Total number of SOCKS requests processed.",
	})
	SocksRequestsSuccessTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "socks",
		Name:      "requests_success_total",
		Help:      "Total number of successful SOCKS connections.",
	})
	SocksRequestsFailedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "socks",
		Name:      "requests_failed_total",
		Help:      "Total number of failed SOCKS connections.",
	})
)

var (
	UpstreamProxyActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "upstream_proxy",
		Name:      "active",
		Help:      "Indicates if an upstream proxy is active (1 for active, 0 for inactive).",
	},
		[]string{"proxy_address"},
	)
	UpstreamProxyResponseTime = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "upstream_proxy",
		Name:      "response_time_seconds",
		Help:      "Last health check response time for an upstream proxy in seconds.",
	},
		[]string{"proxy_address"},
	)
	UpstreamProxySuccessTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "upstream_proxy",
		Name:      "success_total",
		Help:      "Total number of successful connections via an upstream proxy.",
	},
		[]string{"proxy_address"},
	)
	UpstreamProxyFailTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "upstream_proxy",
		Name:      "fail_total",
		Help:      "Total number of failed connections via an upstream proxy.",
	},
		[]string{"proxy_address"},
	)
)

type PrometheusExporter struct {
	pool            *proxypool.Pool
	listenAddress   string
	proxyMetricsMap sync.Map 
}

func NewPrometheusExporter(pool *proxypool.Pool, listenAddress string) *PrometheusExporter {
	return &PrometheusExporter{
		pool:          pool,
		listenAddress: listenAddress,
	}
}

func (pe *PrometheusExporter) Start() {
	if pe.listenAddress == "" {
		log.Println("Prometheus metrics endpoint is disabled (no listen address specified).")
		return
	}

	go func() {
		log.Printf("Starting Prometheus metrics HTTP server on %s/metrics", pe.listenAddress)
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(pe.listenAddress, nil); err != nil {
			log.Printf("Error starting Prometheus metrics HTTP server: %v", err)
		}
	}()
}

func (pe *PrometheusExporter) UpdateProxyMetrics() {
	proxies := pe.pool.GetProxiesSnapshot()
	for _, p := range proxies {
		p.Mu.RLock() 
		addr := p.Address
		isActive := p.IsActive
		responseTime := p.ResponseTime.Seconds()
		p.Mu.RUnlock()

		if isActive {
			UpstreamProxyActive.WithLabelValues(addr).Set(1)
		} else {
			UpstreamProxyActive.WithLabelValues(addr).Set(0)
		}
		UpstreamProxyResponseTime.WithLabelValues(addr).Set(responseTime)
	}
}