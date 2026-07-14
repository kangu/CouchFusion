package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const DefaultMaxBodyBytes int64 = 1 << 20

type Options struct {
	Port         int
	MaxBodyBytes int64
	APIHandler   http.Handler
	Application  Application
	Listener     net.Listener
	Random       io.Reader
}

type LaunchInfo struct {
	Address string `json:"address"`
	URL     string `json:"url"`
}

type Server struct {
	Handler http.Handler

	options     Options
	httpServer  *http.Server
	sessions    *sessionManager
	launchToken string

	mu        sync.Mutex
	listener  net.Listener
	started   bool
	serveDone chan struct{}
	serveErr  error
}

func NewServer(options Options) (*Server, error) {
	if options.Port < 0 || options.Port > 65535 {
		return nil, errors.New("port must be between 0 and 65535")
	}
	if options.MaxBodyBytes <= 0 {
		options.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if options.APIHandler != nil && options.Application != nil {
		return nil, errors.New("APIHandler and Application are mutually exclusive")
	}
	random := options.Random
	if random == nil {
		random = rand.Reader
	}
	launchToken, err := randomToken(random)
	if err != nil {
		return nil, fmt.Errorf("create launch token: %w", err)
	}
	sessions := newSessionManager(random, launchToken)
	server := &Server{
		options:     options,
		sessions:    sessions,
		launchToken: launchToken,
		serveDone:   make(chan struct{}),
	}
	server.Handler = server.routes()
	server.httpServer = &http.Server{
		Handler:           server.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	return server, nil
}

func (s *Server) Start(ctx context.Context) (LaunchInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return LaunchInfo{}, errors.New("UI server is already started")
	}
	listener := s.options.Listener
	var err error
	if listener == nil {
		listener, err = net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.options.Port)))
		if err != nil {
			return LaunchInfo{}, fmt.Errorf("listen on loopback: %w", err)
		}
	}
	if !ipv4LoopbackListener(listener) {
		_ = listener.Close()
		return LaunchInfo{}, errors.New("UI server listener must use 127.0.0.1")
	}
	s.listener = listener
	s.started = true
	address := listener.Addr().String()
	launchURL := "http://" + address + "/?token=" + url.QueryEscape(s.launchToken)
	go func() {
		err := s.httpServer.Serve(listener)
		s.mu.Lock()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.serveErr = err
		}
		close(s.serveDone)
		s.mu.Unlock()
	}()
	if ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = s.Shutdown(shutdownContext)
		}()
	}
	return LaunchInfo{Address: address, URL: launchURL}, nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if !started {
		return nil
	}
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}
	select {
	case <-s.serveDone:
		s.mu.Lock()
		err := s.serveErr
		s.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func ipv4LoopbackListener(listener net.Listener) bool {
	address, ok := listener.Addr().(*net.TCPAddr)
	return ok && address.IP.To4() != nil && address.IP.Equal(net.IPv4(127, 0, 0, 1))
}

func randomToken(random io.Reader) (string, error) {
	value := make([]byte, 32)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
