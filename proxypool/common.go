package proxypool

import (
	"context"
	"net"
	px "golang.org/x/net/proxy"
)

// DialContext используется как ProxyPool, так и Dialer.
// Он экспортируемый, так как Dialer из другого пакета будет его использовать.
func DialContext(ctx context.Context, dialer px.Dialer, network, address string) (net.Conn, error) {
	var conn net.Conn
	var err error

	done := make(chan struct{})
	go func() {
		conn, err = dialer.Dial(network, address)
		close(done)
	}()

	select {
	case <-ctx.Done():
		if conn != nil {
			conn.Close()
		}
		return nil, ctx.Err()
	case <-done:
		return conn, err
	}
}