package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zeelna/linko-starter/internal/logger"
	"github.com/zeelna/linko-starter/internal/store"
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
	// Step 1: Creating non-global loggers
	logFileEnv := os.Getenv("LINKO_LOG_FILE")                           // searching for environment variable value
	myLogger, loggerCleanup, err := logger.InitializeLogger(logFileEnv) // new logger instance
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		return 1
	} // Exit program, we require logs to be written, otherwise halt operations

	// Step 2: Invoking 'defer' to release the bufferedWriter and file-handle before program exits.
	// IMPORTANT: wrapper that calls *bufio.Writer.Flush() and *File.Close() to clean up and handles its error.
	defer func() { // // Must be wrapped in such an anonymous function to avoid losing the error variable.
		if err := loggerCleanup(); err != nil {
			// _, _ = fmt.Fprintf(os.Stderr, "Failed to clean up logging resources: %v\n", err)
			myLogger.Error("Failed to clean up logging resources", slog.Any("error", err))
		} else {
			// _, _ = fmt.Fprintf(os.Stderr, "Successfully cleaned up logging resources\n")
			myLogger.Debug("Successfully cleaned up logging resources")
		}
	}() // if-else block allows avoiding contradicting print statements.

	// Step 3: Running the server.
	st, err := store.New(dataDir, myLogger)
	if err != nil {
		myLogger.Error("failed to create store", slog.Any("error", err))
		return 1
	}
	s := newServer(*st, myLogger, httpPort, cancel)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		myLogger.Error("failed to shutdown server", slog.Any("error", err))
		return 1
	}
	if serverErr != nil {
		myLogger.Error("server error", slog.Any("error", serverErr))
		return 1
	}
	return 0
}
