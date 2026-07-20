package main

import (
	"context"
	"flag"
	"fmt"
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
	// Step 1: Creating non-global loggers
	logFileEnv := os.Getenv("LINKO_LOG_FILE")                  // searching for environment variable value
	logger, loggerCleanup, err := initializeLogger(logFileEnv) // new logger instance
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		return 1
	} // Exit program, we require logs to be written, otherwise halt operations

	// Step 2: Invoking 'defer' to release the bufferedWriter and file-handle before program exits.
	// IMPORTANT: wrapper that calls *bufio.Writer.Flush() and *File.Close() to clean up and handles its error.
	defer func() { // // Must be wrapped in such an anonymous function to avoid losing the error variable.
		if err := loggerCleanup(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Failed to clean up logging resources: %v\n", err)
		} else {
			_, _ = fmt.Fprintf(os.Stderr, "Successfully cleaned up logging resources\n")
		}
	}() // if-else block allows avoiding contradicting print statements.

	// Step 3: Running the server.
	st, err := store.New(dataDir, logger)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to create store: %v", err))
		return 1
	}
	s := newServer(*st, logger, httpPort, cancel)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Error(fmt.Sprintf("failed to shutdown server: %v", err))
		return 1
	}
	if serverErr != nil {
		logger.Error(fmt.Sprintf("server error: %v", serverErr))
		return 1
	}
	return 0
}
