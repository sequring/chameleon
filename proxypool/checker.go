// proxypool/checker.go
package proxypool

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"strings"
	// "sync" // Больше не нужен здесь, если checkAllProxies удален или пуст
	"time"

	px "golang.org/x/net/proxy"
)

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

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: hostNameForTLS,
		MinVersion: tls.VersionTLS12,
	})

	if dl, ok := checkCtx.Deadline(); ok {
		if err := conn.SetDeadline(dl); err != nil {
			log.Printf("Proxy %s: failed to set deadline for TLS handshake: %v", addrToCheck, err)
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