package web

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

type zeroReader struct{}

func (zeroReader) Read(value []byte) (int, error) {
	clear(value)
	return len(value), nil
}

func TestDevelopmentFrontendRequiresSessionAndLeavesAPIWithGo(t *testing.T) {
	frontendCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		frontendCalls++
		_, _ = io.WriteString(writer, "<!doctype html><script>window.dev=true</script>")
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewDevelopmentProxy(target, zeroReader{})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Options{FrontendHandler: proxy})
	if err != nil {
		t.Fatal(err)
	}

	unauthenticated := performRequest(server, http.MethodGet, "/", nil, nil)
	if unauthenticated.Code != http.StatusUnauthorized || frontendCalls != 0 {
		t.Fatalf("unauthenticated = %d, calls = %d", unauthenticated.Code, frontendCalls)
	}
	cookie := authenticatedCookie(t, server)
	frontend := performRequest(server, http.MethodGet, "/workspace", nil, cookie)
	if frontend.Code != http.StatusOK || frontendCalls != 1 {
		t.Fatalf("frontend = %d, calls = %d", frontend.Code, frontendCalls)
	}
	api := performRequest(server, http.MethodGet, "/api/v1/bootstrap", nil, cookie)
	if api.Code != http.StatusOK || frontendCalls != 1 {
		t.Fatalf("api = %d, calls = %d", api.Code, frontendCalls)
	}
}

func TestDevelopmentProxyAddsNonceWithoutChangingCSS(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/style.css" {
			writer.Header().Set("Content-Type", "text/css")
			_, _ = io.WriteString(writer, "body{color:red}")
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(writer, "<!doctype html><script type=\"module\">dev()</script>")
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewDevelopmentProxy(target, zeroReader{})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Options{FrontendHandler: proxy, Random: zeroReader{}})
	if err != nil {
		t.Fatal(err)
	}
	cookie := authenticatedCookie(t, server)

	html := performRequest(server, http.MethodGet, "/", nil, cookie)
	match := regexp.MustCompile(`script-src 'self' 'nonce-([^']+)'`).FindStringSubmatch(html.Header().Get("Content-Security-Policy"))
	if len(match) != 2 || !strings.Contains(html.Body.String(), `nonce="`+match[1]+`"`) {
		t.Fatalf("CSP = %q, body = %s", html.Header().Get("Content-Security-Policy"), html.Body.String())
	}
	if !strings.Contains(html.Header().Get("Content-Security-Policy"), "connect-src 'self' ws:") {
		t.Fatalf("CSP does not allow same-origin HMR: %q", html.Header().Get("Content-Security-Policy"))
	}

	css := performRequest(server, http.MethodGet, "/style.css", nil, cookie)
	if css.Body.String() != "body{color:red}" {
		t.Fatalf("CSS = %q", css.Body.String())
	}
}

func TestDevelopmentProxyRejectsNonLoopbackTarget(t *testing.T) {
	target, err := url.Parse("http://example.com:3000")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDevelopmentProxy(target, zeroReader{}); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("error = %v", err)
	}
}

func TestDevelopmentProxyForwardsAuthenticatedWebSocketUpgrade(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.EqualFold(request.Header.Get("Connection"), "upgrade") || !strings.EqualFold(request.Header.Get("Upgrade"), "websocket") {
			http.Error(writer, "upgrade required", http.StatusBadRequest)
			return
		}
		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			t.Error("upstream response writer cannot hijack")
			return
		}
		connection, buffered, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer connection.Close()
		_, _ = buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = buffered.Flush()
		message, err := buffered.ReadString('\n')
		if err != nil {
			t.Errorf("read upgraded message: %v", err)
			return
		}
		if message != "ping\n" {
			t.Errorf("upgraded message = %q", message)
			return
		}
		_, _ = buffered.WriteString("pong\n")
		_ = buffered.Flush()
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	frontend, err := NewDevelopmentProxy(target, zeroReader{})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Options{FrontendHandler: frontend, Random: zeroReader{}})
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(server.Handler)
	defer proxy.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	exchange, err := client.Get(proxy.URL + "/?token=" + server.launchToken)
	if err != nil {
		t.Fatal(err)
	}
	defer exchange.Body.Close()
	cookies := exchange.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", proxyURL.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = fmt.Fprintf(connection, "GET /hmr HTTP/1.1\r\nHost: %s\r\nCookie: %s=%s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", proxyURL.Host, cookies[0].Name, cookies[0].Value)
	reader := bufio.NewReader(connection)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "101 Switching Protocols") {
		t.Fatalf("status = %q", status)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}
	_, _ = connection.Write([]byte("ping\n"))
	message, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if message != "pong\n" {
		t.Fatalf("upgraded response = %q", message)
	}
}
