package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

// DONE: Removed old logger and used Dependecy Injection
// before: Global logger to send logs to os.Stderr. Useful to steer clear from clogging up the main output of the program
//var logger = log.New(os.Stderr, "DEBUG: ", log.LstdFlags)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()
	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {
	// Creating non-global loggers
	// #1 Standard Logger (example: INFO: <timestamp> Linko is shutting down.)
	standardLogger := log.New(os.Stderr, "DEBUG: ", log.LstdFlags)
	accessLogFileHandler, err := os.OpenFile("linko.access.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		standardLogger.Printf("failed to create access logger: %v", err)
		return 1
	}
	// #2 Access Logger (example: DEBUG: <timestamp> GET / )
	accessLogger := log.New(accessLogFileHandler, "INFO: ", log.LstdFlags)

	// Running the server.
	st, err := store.New(dataDir, standardLogger)
	if err != nil {
		standardLogger.Printf("failed to create store: %v", err)
		return 1
	}
	s := newServer(*st, accessLogger, standardLogger, httpPort, cancel)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		standardLogger.Printf("failed to shutdown server: %v", err)
		return 1
	}
	if serverErr != nil {
		standardLogger.Printf("server error: %v", serverErr)
		return 1
	}
	return 0
}
