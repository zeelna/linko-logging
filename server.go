package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"

	"boot.dev/linko/internal/store"
)

type server struct {
	httpServer *http.Server
	store      store.Store
	cancel     context.CancelFunc
	logger     *log.Logger
}

func newServer(store store.Store, logger *log.Logger, port int, cancel context.CancelFunc) *server {
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
		Handler: requestLogger(logger)(mux),
	} // + leveraging fact that 'mux' of type '*ServerMux' has method .ServeHTTP() similar to 'HandlerFunc' inside mux.Handle()

	s := &server{
		httpServer: srv,
		store:      store,
		cancel:     cancel,
		logger:     logger,
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
	s.logger.Printf("Linko is running on http://localhost:%d", port)

	// Blocking call, 'ln' listener accepts HTTP requests. Therefore custom logger must reside above this next codeline:
	if err := s.httpServer.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *server) shutdown(ctx context.Context) error {
	// Log message:
	s.logger.Println("Linko is shutting down")

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

// Helper to set the destination(s) of the all log entries based on whether 'LINKO_LOG_FILE' environment variable is set.
func initializeLogger() *log.Logger {
	// Assume that in production, Linko has a LINKO_LOG_FILE environment variable set.
	// In local development and staging, it is not set.
	logFileEnv := os.Getenv("LINKO_LOG_FILE")

	// If LINKO_LOG_FILE environment variable is not set, the logger only write to STDERR.
	if logFileEnv == "" {
		logger := log.New(os.Stderr, "", log.LstdFlags)
		return logger
	}
	// Otherwise if LINKO_LOG_FILE is set, it should write both to file and STDERR.
	file, err := os.OpenFile(logFileEnv, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		log.Fatalf("failed to open log file: %v", err)
	}
	// You’re closing the file before the logger writes to it. Therefore, comment out this section.
	//So MultiWriter is holding a closed file handle. That’s why stderr works, but the file stays empty.
	// if err := file.Close(); err != nil {
	// 	log.Fatalf("failed to close log file: %v", err)
	// }
	multiWriter := io.MultiWriter(os.Stderr, file)
	logger := log.New(multiWriter, "", log.LstdFlags)
	return logger
}

// Compile to Go code (go build -o myprogram)
// go build .

// Set the environment variable by using this command in your shell (and run the Go compiled code):
// LINKO_LOG_FILE=linko.access.log go run .
