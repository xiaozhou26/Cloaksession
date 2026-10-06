package browser

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func socksClient(t *testing.T, address string) net.Conn {
	t.Helper()
	c, e := net.DialTimeout("tcp", address, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	_, e = c.Write([]byte{5, 1, 0})
	if e != nil {
		t.Fatal(e)
	}
	var greeting [2]byte
	if _, e = io.ReadFull(c, greeting[:]); e != nil || greeting != [2]byte{5, 0} {
		t.Fatal(greeting, e)
	}
	return c
}
func connectRequest() []byte {
	host := []byte("example.test")
	return append(append([]byte{5, 1, 0, 3, byte(len(host))}, host...), 0, 80)
}
func TestHTTPProxyAuthAndBufferedTunnel(t *testing.T) {
	upstream, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer upstream.Close()
	result := make(chan error, 1)
	go func() {
		c, e := upstream.Accept()
		if e != nil {
			result <- e
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		r, e := http.ReadRequest(bufio.NewReader(c))
		if e != nil {
			result <- e
			return
		}
		if r.Method != "CONNECT" || r.Host != "example.test:80" || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:secret")) {
			result <- fmt.Errorf("bad CONNECT: %v", r)
			return
		}
		_, e = c.Write([]byte("HTTP/1.1 200 Connection established\r\nX-Test: yes\r\n\r\nTUNNEL"))
		result <- e
		_, _ = io.Copy(c, c)
	}()
	bridge, e := startProxy(proxyConfig{scheme: "http", address: upstream.Addr().String(), username: "alice", password: "secret"})
	if e != nil {
		t.Fatal(e)
	}
	defer bridge.Close()
	c := socksClient(t, bridge.listener.Addr().String())
	defer c.Close()
	_, _ = c.Write(connectRequest())
	reply := make([]byte, 16)
	if _, e = io.ReadFull(c, reply); e != nil {
		t.Fatal(e)
	}
	if reply[1] != 0 || string(reply[10:]) != "TUNNEL" {
		t.Fatal(reply)
	}
	if e = <-result; e != nil {
		t.Fatal(e)
	}
	_, _ = c.Write([]byte("echo"))
	buf := make([]byte, 4)
	if _, e = io.ReadFull(c, buf); e != nil || string(buf) != "echo" {
		t.Fatal(string(buf), e)
	}
	bridge.Close()
	if _, e = c.Read(buf); e == nil {
		t.Fatal("bridge close did not close tunnel")
	}
}
func TestSOCKSProxyAuthenticationAndRemoteDNS(t *testing.T) {
	upstream, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer upstream.Close()
	result := make(chan error, 1)
	go func() {
		c, e := upstream.Accept()
		if e != nil {
			result <- e
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		fail := func(e error) { result <- e }
		var hello [3]byte
		if _, e = io.ReadFull(c, hello[:]); e != nil {
			fail(e)
			return
		}
		if hello != [3]byte{5, 1, 2} {
			fail(fmt.Errorf("wrong greeting %v", hello))
			return
		}
		_, _ = c.Write([]byte{5, 2})
		var auth [2]byte
		_, _ = io.ReadFull(c, auth[:])
		user := make([]byte, int(auth[1]))
		_, _ = io.ReadFull(c, user)
		var size [1]byte
		_, _ = io.ReadFull(c, size[:])
		pass := make([]byte, int(size[0]))
		_, _ = io.ReadFull(c, pass)
		if string(user) != "alice" || string(pass) != "secret" {
			fail(fmt.Errorf("bad credentials"))
			return
		}
		_, _ = c.Write([]byte{1, 0})
		var hdr [4]byte
		_, _ = io.ReadFull(c, hdr[:])
		_, dest, e := readSOCKSAddress(c, hdr[3])
		if e != nil || dest != "example.test:80" || hdr[3] != 3 {
			fail(fmt.Errorf("destination %s: %v", dest, e))
			return
		}
		_, _ = c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
		result <- nil
		_, _ = io.Copy(c, c)
	}()
	bridge, e := startProxy(proxyConfig{scheme: "socks5", address: upstream.Addr().String(), username: "alice", password: "secret"})
	if e != nil {
		t.Fatal(e)
	}
	defer bridge.Close()
	c := socksClient(t, bridge.listener.Addr().String())
	defer c.Close()
	_, _ = c.Write(connectRequest())
	reply := make([]byte, 10)
	if _, e = io.ReadFull(c, reply); e != nil || reply[1] != 0 {
		t.Fatal(reply, e)
	}
	if e = <-result; e != nil {
		t.Fatal(e)
	}
	_, _ = c.Write([]byte("hello"))
	echo := make([]byte, 5)
	if _, e = io.ReadFull(c, echo); e != nil || string(echo) != "hello" {
		t.Fatal(string(echo), e)
	}
}
func TestProxyRejectsUDPAndUnsupportedAuth(t *testing.T) {
	b, e := startProxy(proxyConfig{scheme: "http", address: "127.0.0.1:1"})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	c := socksClient(t, b.listener.Addr().String())
	defer c.Close()
	request := connectRequest()
	request[1] = 3
	_, _ = c.Write(request)
	reply := make([]byte, 10)
	if _, e = io.ReadFull(c, reply); e != nil || reply[1] != 7 {
		t.Fatal(reply, e)
	}
	p, e := parseProxy("socks5://alice:p%40ss@[::1]:1080")
	if e != nil || p.password != "p@ss" || !strings.HasPrefix(p.address, "[::1]") {
		t.Fatal(p, e)
	}
}

func TestProxyLocationParsingAndTimeout(t *testing.T) {
	for _, test := range []struct {
		body  string
		valid bool
	}{{`{"latitude":12.5,"longitude":-40.25}`, true}, {`{"error":true}`, false}, {`{"latitude":"bad","longitude":0}`, false}} {
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Host != "geo.example" {
				t.Errorf("probe bypassed proxy: %s", r.URL)
			}
			fmt.Fprint(w, test.body)
		}))
		location, err := proxyLocation(context.Background(), proxy.URL, "http://geo.example/json")
		proxy.Close()
		if (err == nil) != test.valid {
			t.Fatal(location, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := proxyLocation(ctx, "http://127.0.0.1:1", "http://geo.example/json"); err == nil {
		t.Fatal("cancelled probe succeeded")
	}
}
