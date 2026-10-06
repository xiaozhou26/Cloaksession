package browser

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type proxyConfig struct{ scheme, address, username, password string }
type proxyBridge struct {
	listener    net.Listener
	upstream    proxyConfig
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	connections map[net.Conn]struct{}
	closed      bool
	wg          sync.WaitGroup
}

func parseProxy(v any) (proxyConfig, error) {
	m := obj(v)
	server := str(m, "server")
	if s, ok := v.(string); ok {
		server = s
	}
	p := proxyConfig{username: str(m, "username"), password: str(m, "password")}
	if server != "" {
		u, e := url.Parse(server)
		if e != nil || u.Hostname() == "" {
			return p, fmt.Errorf("invalid proxy URL")
		}
		p.scheme = strings.ToLower(u.Scheme)
		port := u.Port()
		if port == "" {
			port = "8080"
			if p.scheme == "socks5" {
				port = "1080"
			}
			if p.scheme == "https" {
				port = "443"
			}
		}
		p.address = net.JoinHostPort(u.Hostname(), port)
		if u.User != nil {
			if p.username == "" {
				p.username = u.User.Username()
			}
			if p.password == "" {
				p.password, _ = u.User.Password()
			}
		}
	} else {
		p.scheme = text(m, "type", "http")
		host := strings.Trim(str(m, "host"), "[]")
		port := int(number(m, "port", 0))
		if host == "" || port < 1 || port > 65535 {
			return p, fmt.Errorf("proxy requires host and valid port")
		}
		p.address = net.JoinHostPort(host, strconv.Itoa(port))
	}
	if p.scheme != "socks5" && p.scheme != "http" && p.scheme != "https" {
		return p, fmt.Errorf("unsupported proxy type %q", p.scheme)
	}
	if len(p.username) > 255 || len(p.password) > 255 {
		return p, fmt.Errorf("proxy credentials exceed 255 bytes")
	}
	return p, nil
}
func startProxy(p proxyConfig) (*proxyBridge, error) {
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &proxyBridge{listener: l, upstream: p, ctx: ctx, cancel: cancel, connections: map[net.Conn]struct{}{}}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			if !b.track(c) {
				continue
			}
			b.wg.Add(1)
			go func() { defer b.wg.Done(); defer b.untrack(c); b.serve(c) }()
		}
	}()
	return b, nil
}
func (b *proxyBridge) URL() string { return "socks5://" + b.listener.Addr().String() }
func (b *proxyBridge) track(c net.Conn) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		_ = c.Close()
		return false
	}
	b.connections[c] = struct{}{}
	return true
}
func (b *proxyBridge) untrack(c net.Conn) {
	_ = c.Close()
	b.mu.Lock()
	delete(b.connections, c)
	b.mu.Unlock()
}
func (b *proxyBridge) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.closed = true
	b.cancel()
	_ = b.listener.Close()
	for c := range b.connections {
		_ = c.Close()
	}
	b.mu.Unlock()
	b.wg.Wait()
}
func readSOCKSAddress(r io.Reader, atyp byte) ([]byte, string, error) {
	var n int
	prefix := []byte{atyp}
	switch atyp {
	case 1:
		n = 4
	case 4:
		n = 16
	case 3:
		var size [1]byte
		if _, e := io.ReadFull(r, size[:]); e != nil {
			return nil, "", e
		}
		n = int(size[0])
		prefix = append(prefix, size[0])
	default:
		return nil, "", fmt.Errorf("unsupported SOCKS address type")
	}
	data := make([]byte, n+2)
	if _, e := io.ReadFull(r, data); e != nil {
		return nil, "", e
	}
	host := string(data[:n])
	if atyp != 3 {
		host = net.IP(data[:n]).String()
	}
	if n == 0 {
		return nil, "", fmt.Errorf("empty SOCKS destination")
	}
	return append(prefix, data...), net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(data[n:])))), nil
}
func (b *proxyBridge) serve(c net.Conn) {
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	var hello [2]byte
	if _, e := io.ReadFull(c, hello[:]); e != nil || hello[0] != 5 {
		return
	}
	methods := make([]byte, int(hello[1]))
	if _, e := io.ReadFull(c, methods); e != nil {
		return
	}
	accepted := false
	for _, m := range methods {
		accepted = accepted || m == 0
	}
	if !accepted {
		_, _ = c.Write([]byte{5, 255})
		return
	}
	if _, e := c.Write([]byte{5, 0}); e != nil {
		return
	}
	var hdr [4]byte
	if _, e := io.ReadFull(c, hdr[:]); e != nil || hdr[0] != 5 {
		return
	}
	address, dest, e := readSOCKSAddress(c, hdr[3])
	if e != nil {
		_, _ = c.Write([]byte{5, 8, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	if hdr[1] != 1 || hdr[2] != 0 {
		_, _ = c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	ctx, cancel := context.WithTimeout(b.ctx, 20*time.Second)
	defer cancel()
	up, e := b.dial(ctx, address, dest)
	if e != nil {
		_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer b.untrack(up)
	if _, e = c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); e != nil {
		return
	}
	_ = c.SetDeadline(time.Time{})
	_ = up.SetDeadline(time.Time{})
	done := make(chan struct{})
	go func() { _, _ = io.Copy(up, c); halfClose(up); close(done) }()
	_, _ = io.Copy(c, up)
	halfClose(c)
	_ = c.Close()
	_ = up.Close()
	<-done
}
func halfClose(c net.Conn) {
	if v, ok := c.(interface{ CloseWrite() error }); ok {
		_ = v.CloseWrite()
	}
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (b *proxyBridge) dial(ctx context.Context, address []byte, dest string) (conn net.Conn, err error) {
	d := net.Dialer{}
	conn, err = d.DialContext(ctx, "tcp", b.upstream.address)
	if err != nil {
		return nil, err
	}
	if !b.track(conn) {
		return nil, fmt.Errorf("proxy closed")
	}
	original := conn
	defer func() {
		if err != nil {
			b.untrack(original)
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	p := b.upstream
	if p.scheme == "socks5" {
		methods := []byte{5, 1, 0}
		if p.username != "" || p.password != "" {
			methods = []byte{5, 1, 2}
		}
		if _, err = conn.Write(methods); err != nil {
			return nil, err
		}
		var reply [2]byte
		if _, err = io.ReadFull(conn, reply[:]); err != nil {
			return nil, err
		}
		if reply[0] != 5 {
			return nil, fmt.Errorf("invalid upstream SOCKS version")
		}
		if reply[1] == 2 && methods[2] == 2 {
			auth := append([]byte{1, byte(len(p.username))}, []byte(p.username)...)
			auth = append(auth, byte(len(p.password)))
			auth = append(auth, []byte(p.password)...)
			if _, err = conn.Write(auth); err != nil {
				return nil, err
			}
			if _, err = io.ReadFull(conn, reply[:]); err != nil {
				return nil, err
			}
			if reply[0] != 1 || reply[1] != 0 {
				return nil, fmt.Errorf("SOCKS authentication rejected")
			}
		} else if reply[1] != 0 || methods[2] != 0 {
			return nil, fmt.Errorf("SOCKS authentication method rejected")
		}
		if _, err = conn.Write(append([]byte{5, 1, 0}, address...)); err != nil {
			return nil, err
		}
		var hdr [4]byte
		if _, err = io.ReadFull(conn, hdr[:]); err != nil {
			return nil, err
		}
		if hdr[0] != 5 || hdr[1] != 0 {
			return nil, fmt.Errorf("SOCKS CONNECT rejected (%d)", hdr[1])
		}
		if _, _, err = readSOCKSAddress(conn, hdr[3]); err != nil {
			return nil, err
		}
		return conn, nil
	}
	if p.scheme == "https" {
		host, _, _ := net.SplitHostPort(p.address)
		tc := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err = tc.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		conn = tc
	}
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: dest}, Host: dest, Header: make(http.Header)}
	if p.username != "" || p.password != "" {
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(p.username+":"+p.password)))
	}
	if err = req.Write(conn); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	resp, e := http.ReadResponse(reader, req)
	if e != nil {
		return nil, e
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP proxy CONNECT rejected (%d)", resp.StatusCode)
	}
	wrapped := &bufferedConn{Conn: conn, reader: reader}
	b.mu.Lock()
	delete(b.connections, original)
	if b.closed {
		b.mu.Unlock()
		_ = original.Close()
		return nil, fmt.Errorf("proxy closed")
	}
	b.connections[wrapped] = struct{}{}
	b.mu.Unlock()
	return wrapped, nil
}

func proxyLocation(ctx context.Context, proxyURL, endpoint string) (object, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{Proxy: http.ProxyURL(u)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy location HTTP %d", response.StatusCode)
	}
	var result object
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return nil, err
	}
	for _, key := range []string{"latitude", "longitude"} {
		if _, ok := result[key].(float64); !ok {
			return nil, fmt.Errorf("proxy location missing %s", key)
		}
	}
	return result, nil
}
