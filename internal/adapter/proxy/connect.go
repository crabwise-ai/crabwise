package proxy

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	p.metrics.TotalRequests.Add(1)
	p.metrics.ActiveConnections.Add(1)
	defer p.metrics.ActiveConnections.Add(-1)

	host := r.Host
	if host == "" {
		host = r.URL.Host
	}

	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}

	p.providersMu.RLock()
	_, _, known := p.router.ResolveByDomain(hostname)
	p.providersMu.RUnlock()

	if known {
		w.WriteHeader(http.StatusOK)

		clientConn, err := hijack(w)
		if err != nil {
			log.Printf("proxy: CONNECT hijack failed for %s: %v", host, err)
			return
		}
		p.mitmConnect(clientConn, hostname)
	} else {
		p.tunnelConnect(w, host)
	}
}

func (p *Proxy) mitmConnect(clientConn net.Conn, hostname string) {
	defer clientConn.Close()

	cert, err := p.certCache.GetOrCreate(hostname)
	if err != nil {
		log.Printf("proxy: MITM cert generation failed for %s: %v", hostname, err)
		return
	}

	tlsConn := tls.Server(clientConn, &tls.Config{
		Certificates: []tls.Certificate{*cert},
		NextProtos:   []string{"http/1.1"},
	})
	if err := tlsConn.Handshake(); err != nil {
		log.Printf("proxy: MITM TLS handshake failed for %s: %v", hostname, err)
		return
	}

	srv := &http.Server{
		Handler: http.HandlerFunc(p.handleProxy),
	}
	srv.SetKeepAlivesEnabled(false)
	srv.Serve(newSingleConnListener(tlsConn))
}

func (p *Proxy) tunnelConnect(w http.ResponseWriter, targetAddr string) {
	if !strings.Contains(targetAddr, ":") {
		targetAddr = targetAddr + ":443"
	}

	remote, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
	if err != nil {
		http.Error(w, fmt.Sprintf("502 Bad Gateway: %v", err), http.StatusBadGateway)
		return
	}
	defer remote.Close()

	w.WriteHeader(http.StatusOK)

	clientConn, err := hijack(w)
	if err != nil {
		log.Printf("proxy: tunnel hijack failed for %s: %v", targetAddr, err)
		return
	}
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(remote, clientConn)
		if tc, ok := remote.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		io.Copy(clientConn, remote)
		if tc, ok := clientConn.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	wg.Wait()
}

func hijack(w http.ResponseWriter) (net.Conn, error) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("response writer does not implement http.Hijacker")
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return nil, fmt.Errorf("hijack: %w", err)
	}
	return conn, nil
}

// singleConnListener is a net.Listener that yields exactly one connection.
// The second Accept blocks until that connection is closed, ensuring
// http.Server.Serve does not return while the handler is still running.
type singleConnListener struct {
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
	addr net.Addr
}

func newSingleConnListener(c net.Conn) *singleConnListener {
	ch := make(chan net.Conn, 1)
	done := make(chan struct{})
	ch <- &sclConn{Conn: c, done: done}
	return &singleConnListener{ch: ch, done: done, addr: c.LocalAddr()}
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	conn, ok := <-l.ch
	if !ok {
		<-l.done
		return nil, net.ErrClosed
	}
	l.once.Do(func() { close(l.ch) })
	return conn, nil
}

func (l *singleConnListener) Close() error {
	l.once.Do(func() { close(l.ch) })
	return nil
}

func (l *singleConnListener) Addr() net.Addr {
	return l.addr
}

// sclConn wraps a net.Conn and signals done when closed.
type sclConn struct {
	net.Conn
	once sync.Once
	done chan struct{}
}

func (c *sclConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { close(c.done) })
	return err
}
