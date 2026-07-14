package web

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) routes() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/bootstrap", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]any{"data": map[string]any{"version": "1"}})
	})
	apiHandler := s.options.APIHandler
	if s.options.Application != nil {
		apiHandler = newAPIHandler(s.options.Application)
	}
	if apiHandler != nil {
		api.Handle("/api/v1/", apiHandler)
	} else {
		api.HandleFunc("/api/v1/", func(writer http.ResponseWriter, _ *http.Request) {
			writeError(writer, http.StatusNotFound, "not_found", "The requested API route was not found")
		})
	}
	frontend := staticHandler()
	if s.options.FrontendHandler != nil {
		frontend = s.options.FrontendHandler
	}

	root := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/" && request.Method == http.MethodGet && request.URL.Query().Has("token") {
			s.exchangeToken(writer, request)
			return
		}
		sessionID, authenticated := s.sessions.authenticate(request)
		if !authenticated {
			if strings.HasPrefix(request.URL.Path, "/api/") {
				writeError(writer, http.StatusUnauthorized, "unauthorized", "Authentication is required")
			} else {
				http.Error(writer, "Authentication is required", http.StatusUnauthorized)
			}
			return
		}
		request = request.WithContext(withSessionID(request.Context(), sessionID))
		if strings.HasPrefix(request.URL.Path, "/api/") {
			s.serveAPI(api, writer, request)
			return
		}
		frontend.ServeHTTP(writer, request)
	})
	return s.recoverPanics(s.securityHeaders(s.validateHost(root)))
}

func (s *Server) exchangeToken(writer http.ResponseWriter, request *http.Request) {
	token := request.URL.Query().Get("token")
	sessionToken, _, err := s.sessions.exchange(token)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "unauthorized", "Launch token is invalid or already consumed")
		return
	}
	s.mu.Lock()
	s.launchToken = ""
	s.mu.Unlock()
	http.SetCookie(writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	writer.Header().Set("Cache-Control", "no-store")
	http.Redirect(writer, request, "/", http.StatusSeeOther)
}

func (s *Server) serveAPI(handler http.Handler, writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if mutationMethod(request.Method) {
		if !sameOrigin(request) {
			writeError(writer, http.StatusForbidden, "forbidden", "Request origin is not allowed")
			return
		}
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeError(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "Mutations require application/json")
			return
		}
		if request.ContentLength > s.options.MaxBodyBytes {
			writeError(writer, http.StatusRequestEntityTooLarge, "request_too_large", "Request body is too large")
			return
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, s.options.MaxBodyBytes+1))
		if err != nil {
			writeError(writer, http.StatusBadRequest, "invalid_request", "Request body could not be read")
			return
		}
		if int64(len(body)) > s.options.MaxBodyBytes {
			writeError(writer, http.StatusRequestEntityTooLarge, "request_too_large", "Request body is too large")
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
	}
	handler.ServeHTTP(writer, request)
}

func (s *Server) validateHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		host := request.Host
		if parsedHost, _, err := net.SplitHostPort(host); err == nil {
			host = parsedHost
		}
		if host != "127.0.0.1" && !strings.EqualFold(host, "localhost") {
			writeError(writer, http.StatusForbidden, "forbidden", "Request host is not allowed")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; frame-src http://127.0.0.1:* http://localhost:*; object-src 'none'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(writer, request)
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recover() != nil {
				if strings.HasPrefix(request.URL.Path, "/api/") {
					writeError(writer, http.StatusInternalServerError, "internal", "An internal server error occurred")
				} else {
					http.Error(writer, "Internal server error", http.StatusInternalServerError)
				}
			}
		}()
		next.ServeHTTP(writer, request)
	})
}

func mutationMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func sameOrigin(request *http.Request) bool {
	if strings.EqualFold(request.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Scheme == "http" && parsed.Host == request.Host && parsed.Path == ""
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}
