package dialer

import (
	"context"
	"errors"
	"log"
	"net"
	"sync/atomic"
	"time"

	"github.com/sequring/chameleon/metrics" 
	"github.com/sequring/chameleon/proxypool"
	px "golang.org/x/net/proxy"
)

type Dialer struct {
	pool         *proxypool.Pool
	commonMetrics *Metrics 
}

func New(pool *proxypool.Pool, commonMetrics *Metrics) *Dialer {
	return &Dialer{
		pool:         pool,
		commonMetrics: commonMetrics,
	}
}

func (d *Dialer) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	metrics.SocksRequestsTotal.Inc()
	atomic.AddUint64(&d.commonMetrics.TotalRequests, 1) 

	proxyCfg, err := d.pool.GetActiveProxy()
	if err != nil {
		metrics.SocksRequestsFailedTotal.Inc()
		atomic.AddUint64(&d.commonMetrics.TotalFailed, 1) 
		log.Printf("Failed to get active proxy: %v", err)
		return nil, err
	}

	var auth *px.Auth
	if proxyCfg.Username != "" {
		auth = &px.Auth{User: proxyCfg.Username, Password: proxyCfg.Password}
	}

	upstreamDialer, err := px.SOCKS5(network, proxyCfg.Address, auth, px.Direct)
	if err != nil {
		metrics.SocksRequestsFailedTotal.Inc()
		atomic.AddUint64(&d.commonMetrics.TotalFailed, 1) 

		metrics.UpstreamProxyFailTotal.WithLabelValues(proxyCfg.Address).Inc()
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
		c, e := proxypool.DialContext(dialProxyCtx, upstreamDialer, network, addr)
		if e != nil {
			errCh <- e
			return
		}
		connCh <- c
	}()

	select {
	case c := <-connCh:
		metrics.SocksRequestsSuccessTotal.Inc()
		atomic.AddUint64(&d.commonMetrics.TotalSuccess, 1)

		metrics.UpstreamProxySuccessTotal.WithLabelValues(proxyCfg.Address).Inc()
		atomic.AddUint32(&proxyCfg.SuccessCount, 1)

		log.Printf("Successfully connected to %s via proxy %s", addr, proxyCfg.Address)
		return c, nil
	case e := <-errCh:
		metrics.SocksRequestsFailedTotal.Inc()
		atomic.AddUint64(&d.commonMetrics.TotalFailed, 1) 

		metrics.UpstreamProxyFailTotal.WithLabelValues(proxyCfg.Address).Inc()
		atomic.AddUint32(&proxyCfg.FailCount, 1) 

		log.Printf("Failed to connect to %s via proxy %s: %v (dialProxyCtx.Err: %v, original_ctx.Err: %v)", addr, proxyCfg.Address, e, dialProxyCtx.Err(), ctx.Err())
		return nil, e
	case <-dialProxyCtx.Done():
		metrics.SocksRequestsFailedTotal.Inc()
		atomic.AddUint64(&d.commonMetrics.TotalFailed, 1) 

		metrics.UpstreamProxyFailTotal.WithLabelValues(proxyCfg.Address).Inc()
		atomic.AddUint32(&proxyCfg.FailCount, 1) 
		
		err := errors.New("dialing " + addr + " via proxy " + proxyCfg.Address + " timed out or was cancelled: " + dialProxyCtx.Err().Error())
		log.Print(err.Error())
		return nil, err
	}
}