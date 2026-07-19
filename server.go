package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"boot.dev/linko/internal/store"
)

type server struct {
	httpServer     *http.Server
	store          store.Store
	cancel         context.CancelFunc
	accessLogger   *log.Logger
	standardLogger *log.Logger
}

func newServer(store store.Store, accessLogger *log.Logger, standardLogger *log.Logger, port int, cancel context.CancelFunc) *server {
	mux := http.NewServeMux()

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	s := &server{
		httpServer:     srv,
		store:          store,
		cancel:         cancel,
		accessLogger:   accessLogger,
		standardLogger: standardLogger,
	}

	mux.Handle("GET /", requestLogger(accessLogger)(http.HandlerFunc(s.handlerIndex)))
	mux.Handle("POST /api/login", requestLogger(accessLogger)(s.authMiddleware(http.HandlerFunc(s.handlerLogin))))
	mux.Handle("POST /api/shorten", requestLogger(accessLogger)(s.authMiddleware(http.HandlerFunc(s.handlerShortenLink))))
	mux.Handle("GET /api/stats", requestLogger(accessLogger)(s.authMiddleware(http.HandlerFunc(s.handlerStats))))
	mux.Handle("GET /api/urls", requestLogger(accessLogger)(s.authMiddleware(http.HandlerFunc(s.handlerListURLs))))
	mux.Handle("GET /{shortCode}", requestLogger(accessLogger)(http.HandlerFunc(s.handlerRedirect)))
	mux.Handle("POST /admin/shutdown", requestLogger(accessLogger)(http.HandlerFunc(s.handlerShutdown)))

	return s
}

func (s *server) start() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	if err := s.httpServer.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	// Log message:
	//  calling the methid 'Addr', then asserting its results to typeof *net.TCPAddr
	tcpAddr := ln.Addr().(*net.TCPAddr)
	port := tcpAddr.Port
	s.standardLogger.Printf("Linko is running on http://localhost:%d", port)

	return nil
}

func (s *server) shutdown(ctx context.Context) error {
	// Log message:
	s.standardLogger.Println("Linko is shutting down")

	return s.httpServer.Shutdown(ctx)
}

func (s *server) handlerShutdown(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("ENV") == "production" {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
	go s.cancel()
}

// Middleware to log with Dependency Injected logger, stored in s.standardLogger
func requestLogger(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			logger.Printf("Served request: %s %s", r.Method, r.URL.Path)
		})
	}
}
