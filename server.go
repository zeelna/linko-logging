package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/zeelna/linko-logging/internal/logger"
	"github.com/zeelna/linko-logging/internal/store"
)

type server struct {
	httpServer *http.Server
	store      store.Store
	cancel     context.CancelFunc
	logger     *slog.Logger
}

func newServer(store store.Store, myLogger *slog.Logger, port int, cancel context.CancelFunc) *server {
	mux := http.NewServeMux()

	/* // Option 1: Logger must be through middleware for each HTTP endpoint
	// example: mux.Handle("POST /api/login", requestLogger(logger)(s.authMiddleware(http.HandlerFunc(s.handlerLogin))))
	// Pros: less reliance on decorator / middleware pattern
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	*/

	// Option 2: Logger added to ALL endpoints,
	// example: mux.Handle("POST /api/login", s.authMiddleware(http.HandlerFunc(s.handlerLogin)))
	// Pros: avoid duplicate logging-middleware calls for each http endpoint request handler methods
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: logger.RequestIDMiddleware(logger.RequestLogger(myLogger)(mux)),
	} // + leveraging fact that 'mux' of type '*ServerMux' has method .ServeHTTP() similar to 'HandlerFunc' inside mux.Handle()

	s := &server{
		httpServer: srv,
		store:      store,
		cancel:     cancel,
		logger:     myLogger,
	}

	mux.Handle("GET /", http.HandlerFunc(s.handlerIndex))
	mux.Handle("POST /api/login", s.authMiddleware(http.HandlerFunc(s.handlerLogin)))
	mux.Handle("POST /api/shorten", s.authMiddleware(http.HandlerFunc(s.handlerShortenLink)))
	mux.Handle("GET /api/stats", s.authMiddleware(http.HandlerFunc(s.handlerStats)))
	mux.Handle("GET /api/urls", s.authMiddleware(http.HandlerFunc(s.handlerListURLs)))
	mux.Handle("GET /{shortCode}", http.HandlerFunc(s.handlerRedirect))
	mux.Handle("POST /admin/shutdown", http.HandlerFunc(s.handlerShutdown))

	return s
}

func (s *server) start() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}

	// Log message:
	//  calling the method 'Addr', then asserting its results to typeof *net.TCPAddr
	tcpAddr := ln.Addr().(*net.TCPAddr)
	port := tcpAddr.Port
	s.logger.Debug(
		fmt.Sprintf("Linko is running on http://localhost:%d", port),
		slog.String("addr", tcpAddr.String()),
		slog.Int("port", port),
	)

	// Blocking call, 'ln' listener accepts HTTP requests. Therefore custom logger must reside above this next codeline:
	if err := s.httpServer.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *server) shutdown(ctx context.Context) error {
	// Log message:
	s.logger.Debug("Linko is shutting down")

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

// Compile to Go code (go build -o myprogram)
// go build .

// Set the environment variable by using this command in your shell (and run the Go compiled code):
// LINKO_LOG_FILE=linko.access.log go run .
