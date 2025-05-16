package proxypool

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	px "golang.org/x/net/proxy"
)

func (p *Pool) checkAllProxies() {
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

func (p *Pool) checkProxy(ctx context.Context, proxyCfg *ProxyConfig) {
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
		proxyCfg.MarkInactive(err)
		return
	}

	targetHost := p.testURL
	hostNameForTLS := targetHost
	if strings.Contains(targetHost, ":") {
		hostNameForTLS, _, err = net.SplitHostPort(targetHost)
		if err != nil {
			log.Printf("Proxy %s: invalid testURL format '%s' for SplitHostPort: %v", proxyCfg.Address, targetHost, err)
			proxyCfg.MarkInactive(err)
			return
		}
	}

	conn, err := DialContext(checkCtx, dialer, "tcp", targetHost)

	if err != nil {
		select {
		case <-checkCtx.Done():
			log.Printf("Proxy %s check timed out or cancelled: %v (underlying error: %v)", proxyCfg.Address, checkCtx.Err(), err)
		default:
			log.Printf("Proxy %s: failed to dial test URL '%s': %v", proxyCfg.Address, targetHost, err)
		}
		proxyCfg.MarkInactive(err)
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
		proxyCfg.MarkInactive(err)
		return
	}

	proxyCfg.MarkActive(time.Since(start))
	log.Printf("Proxy %s is active, response time: %v", proxyCfg.Address, proxyCfg.ResponseTime)
}