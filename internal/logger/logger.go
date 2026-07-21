package logger

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	pkgerr "github.com/pkg/errors"
)

// Middleware to log with Dependency Injected logger, stored in s.standardLogger
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
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

// stackTracer interface to extract stack traces from errors wrapped with pkg/errors:
type stackTracer interface {
	error
	StackTrace() pkgerr.StackTrace
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key != "error" {
		return a
	}
	// 1) Resolve() tells slog to fully resolve the value first.
	// 2) then Any() gives you the wrapped value.
	// 3) then the type assertion to error works
	err, ok := a.Value.Resolve().Any().(error)
	if !ok {
		return a
	}
	// 4) then errors.AsType[stackTracer](err) can find the stack-trace-carrying error
	stackErr, ok := errors.AsType[stackTracer](err)
	if !ok {
		return a
	}
	// 5) then you return a grouped error object instead of one giant string
	return slog.GroupAttrs(
		"error",
		slog.Attr{
			Key:   "message",
			Value: slog.StringValue(stackErr.Error()),
		},
		slog.Attr{
			Key:   "stack_trace",
			Value: slog.StringValue(fmt.Sprintf("%+v", stackErr.StackTrace())),
		},
	)
}

// Buffered Writer must be flushed before the program exits.
type closeFunc func() error

// Helper to set the destination(s) of the all log entries based on whether 'LINKO_LOG_FILE' environment variable is set.
func InitializeLogger(logFileEnv string) (*slog.Logger, closeFunc, error) {

	/* // A more practical approach is a single logger that routes logs to different destinations by level.
	//For example, everything goes to STDERR, but only INFO and higher go to a file.
	// As of Go 1.26, this is easy with slog.NewMultiHandler:
	*/
	// Assume that in production, Linko has a LINKO_LOG_FILE environment variable set.
	// If LINKO_LOG_FILE environment variable is not set, the logger only write to STDERR.
	if logFileEnv == "" {
		// A. log.DEBUG. Create single logger with different destinations by level
		debugHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level:       slog.LevelDebug,
			ReplaceAttr: replaceAttr, // any logged error is accompanied by stack trace + error
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
		Level:       slog.LevelInfo, // ^ Debug and above into FILE ^
		ReplaceAttr: replaceAttr,    // any logged error is accompanied by stack trace + error
	})
	// A. log.Debug (into STDERR). Create single logger with different destinations by level
	debugHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       slog.LevelDebug, // ^ Debug and above into os.Stderr ^
		ReplaceAttr: replaceAttr,     // any logged error is accompanied by stack trace + error
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
