package s3server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/alist-org/alist/v3/pkg/utils"
	log "github.com/sirupsen/logrus"
)

const (
	refreshInterval          = 2 * time.Second
	gracefulShutdownTimeout  = 10 * time.Second
	defaultListenBindAddress = "0.0.0.0"
)

type service struct {
	mu       sync.Mutex
	server   *http.Server
	listener net.Listener
	cfg      *Config
	running  bool
	stopCh   chan struct{}
}

var globalService = &service{}

func Start() {
	globalService.start()
}

func Stop() {
	globalService.stop(context.Background())
}

func Refresh() {
	globalService.refresh()
}

func (s *service) start() {
	s.mu.Lock()
	if s.stopCh != nil {
		s.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	s.stopCh = stopCh
	s.mu.Unlock()

	utils.GoSafe("s3-server-watch", func() {
		s.watch(stopCh)
	})

	s.refresh()
}

func (s *service) watch(stopCh <-chan struct{}) {
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.refresh()
		case <-stopCh:
			return
		}
	}
}

func (s *service) refresh() {
	cfg, err := loadConfig()
	if err != nil {
		log.Errorf("[s3] load config failed: %+v", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !cfg.enabled() {
		if s.running {
			server := s.server
			listener := s.listener
			s.server = nil
			s.listener = nil
			s.running = false
			go shutdownHTTPServer(server, listener)
		}
		s.cfg = cfg
		return
	}

	if s.server == nil || s.listener == nil {
		s.cfg = cfg
		go s.startLocked(cfg)
		return
	}

	if s.cfg != nil && s.cfg.Port == cfg.Port {
		s.cfg = cfg
		return
	}

	oldServer := s.server
	oldListener := s.listener
	s.server = nil
	s.listener = nil
	s.running = false
	s.cfg = cfg

	go func() {
		_ = shutdownHTTPServer(oldServer, oldListener)
		s.startLocked(cfg)
	}()
}

func (s *service) startLocked(cfg *Config) {
	s.mu.Lock()
	if s.stopCh == nil {
		s.mu.Unlock()
		return
	}
	if s.running {
		s.mu.Unlock()
		return
	}
	addr := cfg.listenAddr()
	s.mu.Unlock()

	if addr == "" {
		return
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(defaultListenBindAddress, fmt.Sprintf("%d", cfg.Port)))
	if err != nil {
		log.Errorf("[s3] listen on port %d failed: %+v", cfg.Port, err)
		return
	}

	server := &http.Server{
		Handler:      &handler{},
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
	}

	s.mu.Lock()
	s.server = server
	s.listener = listener
	s.running = true
	s.cfg = cfg
	s.mu.Unlock()

	log.Infof("[s3] listening on %s", listener.Addr().String())
	utils.GoSafe("s3-server-listen", func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			log.Errorf("[s3] serve error: %+v", err)
		}
		s.mu.Lock()
		if s.server == server {
			s.server = nil
			s.listener = nil
			s.running = false
		}
		s.mu.Unlock()
	})
}

func (s *service) stop(ctx context.Context) {
	s.mu.Lock()
	stopCh := s.stopCh
	s.stopCh = nil
	server := s.server
	listener := s.listener
	s.server = nil
	s.listener = nil
	s.running = false
	s.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
	_ = shutdownHTTPServer(server, listener)
}

func shutdownHTTPServer(server *http.Server, listener net.Listener) error {
	if server == nil {
		if listener != nil {
			return listener.Close()
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		if listener != nil {
			_ = listener.Close()
		}
		return err
	}
	if listener != nil {
		return listener.Close()
	}
	return nil
}
