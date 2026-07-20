package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"

	"boot.dev/linko/internal/store"
)

type server struct {
	httpServer *http.Server
	store      store.Store
	cancel     context.CancelFunc
	logger     *slog.Logger
}

func newServer(store store.Store, logger *slog.Logger, port int, cancel context.CancelFunc) *server {
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

// Middleware to log with Dependency Injected logger, stored in s.standardLogger
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			logger.Info(
				"Served request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("client_ip", r.RemoteAddr),
			)
		})
	}
}

// Buffered Writer must be flushed before the program exits.
type closeFunc func() error

// Helper to set the destination(s) of the all log entries based on whether 'LINKO_LOG_FILE' environment variable is set.
func initializeLogger(logFileEnv string) (*slog.Logger, closeFunc, error) {
	/* // A more practical approach is a single logger that routes logs to different destinations by level.
	//For example, everything goes to STDERR, but only INFO and higher go to a file.
	// As of Go 1.26, this is easy with slog.NewMultiHandler:
	*/
	// Assume that in production, Linko has a LINKO_LOG_FILE environment variable set.
	// If LINKO_LOG_FILE environment variable is not set, the logger only write to STDERR.
	if logFileEnv == "" {
		// A. log.DEBUG. Create single logger with different destinations by level
		debugHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}) // ^ Debug and above into os.Stderr ^
		logger := slog.New(debugHandler)
		// Create a non-operational function of type closeFunc due to no bufio.BufferedWriter, and as such, no need to .Flush()
		var closeFn closeFunc = func() error { return nil }
		return logger, closeFn, nil
	}
	// Otherwise if LINKO_LOG_FILE is set, it should write both to file and STDERR.
	//  %%% %%% %%% B. log.Info (into file).  %%% %%% %%%
	file, err := os.OpenFile(logFileEnv, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, err
	}
	/* Logs are written to disk, no matter how large or small the message is.
		- That's potentially a lot of disk I/O, and it can really slow down our entire application
	 	+ use a buffered writer like bufio.Writer around the file.
		// This allows us to write log messages to an in-memory buffer,
		// and then that buffer is only written to disk when it's full.
	*/
	const bufferedBytes = 8192
	bufferedFile := bufio.NewWriterSize(file, bufferedBytes) // buffered bytes, 8192
	infoHandler := slog.NewJSONHandler(bufferedFile, &slog.HandlerOptions{
		Level: slog.LevelInfo, // ^ Debug and above into FILE ^
	})
	// A. log.Debug (into STDERR). Create single logger with different destinations by level
	debugHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug, // ^ Debug and above into os.Stderr ^
	})
	//  %%% %%% %%% %%% %%% %%% %%% %%% %%% %%%
	logger := slog.New(slog.NewMultiHandler(
		debugHandler, // DEBUG and above: into os.Stderr
		infoHandler,  // INFO and above: into bufferedFile (linko.access.log)
	))

	// ----- Safe cleanup (fn-expression) to free resources before program exits ------------
	// Function expression to clear *bufio.Writer and close *File before program exits.
	var closeFn closeFunc = func() error {
		if err := bufferedFile.Flush(); err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		return nil //  <- Happy path of function expression named closeFn
	}
	return logger, closeFn, nil
}

// Compile to Go code (go build -o myprogram)
// go build .

// Set the environment variable by using this command in your shell (and run the Go compiled code):
// LINKO_LOG_FILE=linko.access.log go run .
