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

	"github.com/zeelna/linko-logging/internal/build"
	"github.com/zeelna/linko-logging/internal/logger"
	"github.com/zeelna/linko-logging/internal/store"
)

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
	// Set Runtime Environment:
	env := os.Getenv("ENV")      // Runtime environment name (e.g. production, staging, development)
	hostname, _ := os.Hostname() // Server hostname/domain (e.g. jons-macbook (local), c7a9d8b32c4a (container), or ip-172-31-22-45 (cloud server))

	// Step 0: Creating non-global loggers
	logFileEnv := os.Getenv("LINKO_LOG_FILE")                           // searching for environment variable value
	myLogger, loggerCleanup, err := logger.InitializeLogger(logFileEnv) // new logger instance
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		return 1
	} // Exit program, we require logs to be written, otherwise halt operations

	// Step 1: Immediately after initializing your logger, add these two fields to include Build Information
	myLogger = myLogger.With(
		slog.String("git_sha", build.GitSHA),
		slog.String("build_time", build.BuildTime),
		slog.String("env", env),
		slog.String("hostname", hostname),
	)
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

// -----------------------------------------------------
// IMPORTANT: go run does not support -ldflags variable injection – the variables will remain "unknown" unless you use go build first.

// Build and run with:
/* // #1 Build your app using -ldflags to inject values at link time:
go build \
  -ldflags "-X github.com/zeelna/linko-logging/internal/build.GitSHA=$(git rev-parse HEAD) -X github.com/zeelna/linko-logging/internal/build.BuildTime=$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
  -o linko

//  #2 Run the prebuilt app with the log file path set:
LINKO_LOG_FILE=linko.access.log ./linko
*/

/* // Build and run in 'dev'
go build \
  -ldflags "-X github.com/zeelna/linko-logging/internal/build.GitSHA=$(git rev-parse HEAD) -X github.com/zeelna/linko-logging/internal/build.BuildTime=$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
  -o linko &&
LINKO_LOG_FILE=linko.access.log ENV=development ./linko
*/

// -----------------------------------------------------
