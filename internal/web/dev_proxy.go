package web

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

const (
	developmentBaseCSP = "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; object-src 'none'; connect-src 'self' ws:; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'"
	developmentHTMLCSP = "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; object-src 'none'; connect-src 'self' ws:; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'nonce-%s'"
)

func NewDevelopmentProxy(target *url.URL, random io.Reader) (http.Handler, error) {
	if target == nil || target.Scheme != "http" || target.Hostname() != "127.0.0.1" || target.Port() == "" {
		return nil, errors.New("development UI target must be an HTTP IPv4 loopback address with an explicit port")
	}
	if random == nil {
		random = rand.Reader
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.SetXForwarded()
			request.Out.Host = target.Host
			request.Out.Header.Del("Accept-Encoding")
		},
		ModifyResponse: func(response *http.Response) error {
			response.Header.Set("Content-Security-Policy", developmentBaseCSP)
			mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
			if err != nil || mediaType != "text/html" {
				return nil
			}
			document, err := io.ReadAll(response.Body)
			if err != nil {
				return fmt.Errorf("read Nuxt development HTML: %w", err)
			}
			if err := response.Body.Close(); err != nil {
				return fmt.Errorf("close Nuxt development HTML: %w", err)
			}
			nonce, err := randomToken(random)
			if err != nil {
				return fmt.Errorf("secure Nuxt development HTML: %w", err)
			}
			document = []byte(strings.ReplaceAll(string(document), "<script", `<script nonce="`+nonce+`"`))
			response.Body = io.NopCloser(bytes.NewReader(document))
			response.ContentLength = int64(len(document))
			response.Header.Set("Content-Length", strconv.Itoa(len(document)))
			response.Header.Del("Content-Encoding")
			response.Header.Del("ETag")
			response.Header.Set("Content-Security-Policy", fmt.Sprintf(developmentHTMLCSP, nonce))
			return nil
		},
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			writer.Header().Set("Content-Security-Policy", developmentBaseCSP)
			http.Error(writer, "Nuxt development server is unavailable", http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Del("Content-Security-Policy")
		proxy.ServeHTTP(writer, request)
	}), nil
}
