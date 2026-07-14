package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServerStartsOnlyOnIPv4Loopback(t *testing.T) {
	server, err := NewServer(Options{Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	launch, err := server.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	if !strings.HasPrefix(launch.Address, "127.0.0.1:") || !strings.HasPrefix(launch.URL, "http://127.0.0.1:") {
		t.Fatalf("launch = %#v", launch)
	}
}

func TestLaunchTokenHasAtLeast256BitsAndIsConsumedOnce(t *testing.T) {
	server := newSecurityTestServer(t, nil)
	raw, err := base64.RawURLEncoding.DecodeString(server.launchToken)
	if err != nil || len(raw) < 32 {
		t.Fatalf("launch token decodes to %d bytes, error = %v", len(raw), err)
	}

	first := exchangeLaunchToken(t, server)
	if first.Code != http.StatusSeeOther || first.Header().Get("Location") != "/" {
		t.Fatalf("first exchange = %d, location %q", first.Code, first.Header().Get("Location"))
	}
	if strings.Contains(first.Header().Get("Location"), "token") {
		t.Fatalf("redirect leaks token: %q", first.Header().Get("Location"))
	}
	cookies := first.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" {
		t.Fatalf("cookies = %#v", cookies)
	}

	second := exchangeLaunchToken(t, server)
	if second.Code != http.StatusUnauthorized {
		t.Fatalf("second exchange status = %d", second.Code)
	}
}

func TestAuthenticatedBootstrapIsJSONAndUnauthenticatedAPIAndSSEAreRejected(t *testing.T) {
	server := newSecurityTestServer(t, nil)

	for _, path := range []string{"/api/v1/bootstrap", "/api/v1/jobs/job-1/events"} {
		recorder := performRequest(server, http.MethodGet, path, nil, nil)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s status = %d", path, recorder.Code)
		}
	}

	cookie := authenticatedCookie(t, server)
	recorder := performRequest(server, http.MethodGet, "/api/v1/bootstrap", nil, cookie)
	if recorder.Code != http.StatusOK || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("bootstrap = %d %q: %s", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil || envelope["data"] == nil {
		t.Fatalf("bootstrap body = %s, error = %v", recorder.Body.String(), err)
	}
}

func TestServerRejectsForeignHostAndMutationOrigin(t *testing.T) {
	api := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]any{"data": true})
	})
	server := newSecurityTestServer(t, api)
	cookie := authenticatedCookie(t, server)

	hostRequest := httptest.NewRequest(http.MethodGet, "http://attacker.example/api/v1/bootstrap", nil)
	hostRequest.AddCookie(cookie)
	hostRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(hostRecorder, hostRequest)
	if hostRecorder.Code != http.StatusForbidden {
		t.Fatalf("foreign host status = %d", hostRecorder.Code)
	}

	originRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/test", strings.NewReader(`{}`))
	originRequest.Header.Set("Content-Type", "application/json")
	originRequest.Header.Set("Origin", "https://attacker.example")
	originRequest.AddCookie(cookie)
	originRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(originRecorder, originRequest)
	if originRecorder.Code != http.StatusForbidden {
		t.Fatalf("foreign origin status = %d", originRecorder.Code)
	}
}

func TestMutationRequiresJSONAndRejectsOversizedBody(t *testing.T) {
	api := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		writeJSON(writer, http.StatusOK, map[string]any{"data": true})
	})
	server := newSecurityTestServer(t, api)
	cookie := authenticatedCookie(t, server)

	wrongTypeRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/test", strings.NewReader("value=x"))
	wrongTypeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wrongTypeRequest.Header.Set("Origin", "http://127.0.0.1")
	wrongTypeRequest.AddCookie(cookie)
	wrongTypeRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(wrongTypeRecorder, wrongTypeRequest)
	if wrongTypeRecorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("wrong content type status = %d", wrongTypeRecorder.Code)
	}

	large := strings.NewReader(strings.Repeat("x", int(DefaultMaxBodyBytes+1)))
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/test", large)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large body status = %d", recorder.Code)
	}
}

func TestResponsesSetSecurityHeadersAndAuthenticatedSPAFallback(t *testing.T) {
	server := newSecurityTestServer(t, nil)
	launchToken := server.launchToken
	cookie := authenticatedCookie(t, server)
	if server.launchToken != "" {
		t.Fatal("plaintext launch token retained after exchange")
	}
	recorder := performRequest(server, http.MethodGet, "/workspaces/example", nil, cookie)
	if recorder.Code != http.StatusOK || !strings.Contains(strings.ToLower(recorder.Body.String()), "<!doctype html>") {
		t.Fatalf("SPA response = %d: %s", recorder.Code, recorder.Body.String())
	}
	for header, want := range map[string]string{
		"Content-Security-Policy": "default-src 'self'",
		"X-Frame-Options":         "DENY",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
	} {
		if !strings.Contains(recorder.Header().Get(header), want) {
			t.Fatalf("%s = %q, want %q", header, recorder.Header().Get(header), want)
		}
	}
	if strings.Contains(recorder.Body.String(), launchToken) {
		t.Fatal("SPA response contains launch token")
	}
}

func newSecurityTestServer(t *testing.T, api http.Handler) *Server {
	t.Helper()
	server, err := NewServer(Options{APIHandler: api})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func exchangeLaunchToken(t *testing.T, server *Server) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/?token="+server.launchToken, nil)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)
	return recorder
}

func authenticatedCookie(t *testing.T, server *Server) *http.Cookie {
	t.Helper()
	recorder := exchangeLaunchToken(t, server)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("token exchange status = %d: %s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	return cookies[0]
}

func performRequest(server *Server, method, path string, body io.Reader, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://127.0.0.1"+path, body)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://127.0.0.1")
	}
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)
	return recorder
}
