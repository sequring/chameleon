package proxypool

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log"
	"net"
	"strings"
	"time"

	px "golang.org/x/net/proxy"
)

// TLSCheckConfig holds configuration for TLS certificate verification during health checks
type TLSCheckConfig struct {
	// SkipVerify disables certificate verification if set to true.
	// WARNING: Setting this to true makes the connection vulnerable to man-in-the-middle attacks.
	// Only use this for testing or in trusted environments.
	SkipVerify bool

	// RootCAs is an optional pool of root certificates to use for verification.
	// If nil, the system's default root certificates are used.
	RootCAs *x509.CertPool

	// ServerName is used for both SNI and certificate verification.
	// If empty, the hostname from the target URL will be used.
	ServerName string
}

// DefaultTLSCheckConfig returns a secure default configuration for TLS checks
func DefaultTLSCheckConfig() *TLSCheckConfig {
	return &TLSCheckConfig{
		SkipVerify: false,
		RootCAs:    nil, // Use system certs by default
	}
}

// checkAllProxies - этот метод в текущей архитектуре с healthCheckLoopForProxy
// практически не нужен для регулярных проверок. Первоначальные запуски проверок
// происходят при создании ProxyConfig в reloadAndReconcileProxies.
// Оставляем его пустым или удаляем. Если оставить, то он должен быть методом Pool.
/*
func (p *Pool) checkAllProxies() {
	// Логика здесь была бы для принудительного запуска раунда проверок
	// для всех существующих горутин healthCheckLoopForProxy.
	// Это потребовало бы дополнительной синхронизации (например, каналов).
	// В текущей реализации он не используется активно.
	log.Println("ProxyPool: checkAllProxies called (currently a no-op in this architecture).")
}
*/

// checkProxy выполняет одну проверку работоспособности для указанного ProxyConfig.
// Этот метод вызывается из healthCheckLoopForProxy.
// `ctx` - это контекст горутины healthCheckLoopForProxy, который может быть отменен.
func (p *Pool) checkProxy(ctx context.Context, proxyCfg *ProxyConfig) { // Ресивер p *Pool
	start := time.Now()
	checkCtx, cancel := context.WithTimeout(ctx, p.timeout) // Используем p.timeout
	defer cancel()

	var auth *px.Auth
	proxyCfg.Mu.RLock()
	addrToCheck := proxyCfg.Address // Копируем, чтобы не держать мьютекс на время диала
	username := proxyCfg.Username
	password := proxyCfg.Password
	proxyCfg.Mu.RUnlock()

	if username != "" {
		auth = &px.Auth{User: username, Password: password}
	}

	dialer, err := px.SOCKS5("tcp", addrToCheck, auth, px.Direct)
	if err != nil {
		log.Printf("Proxy %s: failed to create SOCKS5 dialer: %v", addrToCheck, err)
		proxyCfg.MarkInactive(err)
		return
	}

	targetHost := p.testURL // Используем p.testURL
	hostNameForTLS := targetHost
	if strings.Contains(targetHost, ":") {
		var port string
		hostNameForTLS, port, err = net.SplitHostPort(targetHost)
		if err != nil {
			log.Printf("Proxy %s: invalid testURL format '%s' for SplitHostPort: %v", addrToCheck, targetHost, err)
			proxyCfg.MarkInactive(err)
			return
		}
		if port == "" { // Если SplitHostPort вернул хост, но порт был ожидаем (например, из-за ошибки в testURL)
			 // это может быть проблемой, если targetHost для dialContext должен содержать порт
			log.Printf("Warning: Proxy %s: port not found in testURL '%s' after SplitHostPort, using original targetHost for dialing.", addrToCheck, targetHost)
		}

	}

	conn, err := DialContext(checkCtx, dialer, "tcp", targetHost) // DialContext из common.go

	if err != nil {
		select {
		case <-checkCtx.Done():
			log.Printf("Proxy %s check for '%s' timed out or cancelled: %v (underlying dial error: %v)", addrToCheck, targetHost, checkCtx.Err(), err)
		default:
			log.Printf("Proxy %s: failed to dial test URL '%s': %v", addrToCheck, targetHost, err)
		}
		proxyCfg.MarkInactive(err)
		return
	}
	defer conn.Close()

	// Get the current TLS config atomically
	tlsConfig, _ := p.tlsCheckConfig.Load().(*TLSCheckConfig)
	if tlsConfig == nil {
		tlsConfig = DefaultTLSCheckConfig()
	}

	// Create a secure TLS configuration for health checks
	tlsCfg := &tls.Config{
		ServerName:         hostNameForTLS,
		InsecureSkipVerify: tlsConfig.SkipVerify,
		RootCAs:           tlsConfig.RootCAs,
		MinVersion:         tls.VersionTLS12,
	}

	// Set up the VerifyConnection callback
	tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
		// Get fresh config in case it was updated
		currentTLSConfig, _ := p.tlsCheckConfig.Load().(*TLSCheckConfig)
		if currentTLSConfig == nil {
			currentTLSConfig = DefaultTLSCheckConfig()
		}

		// If verification is disabled, just log a warning and return
		if currentTLSConfig.SkipVerify {
			log.Printf("WARNING: TLS certificate verification is disabled for proxy %s. This is not recommended for production use.", addrToCheck)
			return nil
		}

		// Standard verification
		opts := x509.VerifyOptions{
			Roots:         currentTLSConfig.RootCAs,
			Intermediates: x509.NewCertPool(),
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}

		if currentTLSConfig.ServerName != "" {
			opts.DNSName = currentTLSConfig.ServerName
		}

		// Add all certificates except the first one (the leaf) to the intermediates pool
		for _, cert := range cs.PeerCertificates[1:] {
			opts.Intermediates.AddCert(cert)
		}

		// Verify the certificate chain
		_, err := cs.PeerCertificates[0].Verify(opts)
		// Only return the error, don't log successful verifications
		return err
	}
	tlsConn := tls.Client(conn, tlsCfg)

	if dl, ok := checkCtx.Deadline(); ok {
		if err := conn.SetDeadline(dl); err != nil {
			// Don't log failed deadline settings as they're not critical
		}
	}

	if err := tlsConn.HandshakeContext(checkCtx); err != nil {
		select {
		case <-checkCtx.Done():
			log.Printf("Proxy %s: TLS handshake to '%s' (SNI: %s) timed out or cancelled: %v (underlying handshake error: %v)", addrToCheck, targetHost, hostNameForTLS, checkCtx.Err(), err)
		default:
			log.Printf("Proxy %s: TLS handshake to '%s' (SNI: %s) failed: %v", addrToCheck, targetHost, hostNameForTLS, err)
		}
		proxyCfg.MarkInactive(err)
		return
	}

	responseTime := time.Since(start)
	proxyCfg.MarkActive(responseTime)
	log.Printf("Proxy %s is active, response time: %v", addrToCheck, responseTime)
}