package logger

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	pkgerr "github.com/pkg/errors"
	"github.com/zeelna/linko-starter/internal/linkoerr"
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

// replaceAttr rewrites the "error" slog.Attr into a structured group.
//
// Steps:
//  1. Resolve() forces slog to fully evaluate the value, and Any() unwraps it
//     so the type assertion to `error` succeeds.
//  2. Build attrs starting with "message" (the error's own text).
//  3. linkoerr.Attrs(err) walks the error chain and returns any structured
//     fields attached via linkoerr.WithAttrs (e.g. "path", "item_no") - these
//     are spread with `...` so each becomes its own field in the group,
//     instead of being nested as a single slice value.
//     (example: store.go's walk() -> linkoerr.WithAttrs(err, "path", ...))
//  4. If the error chain also carries a stack trace (from pkg/errors), append
//     a "stack_trace" field too - this is optional and only added when present.
//  5. GroupAttrs bundles everything into a single "error" group instead of one
//     giant string, so message/attrs/stack_trace are each queryable fields.
func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key != "error" {
		return a
	}
	// 1) Resolve() tells slog to fully resolve the value first. 2) then Any() gives you the wrapped value.
	err, ok := a.Value.Resolve().Any().(error) //  3) then the type assertion to error works
	if !ok {
		return a
	}
	// declares a new slice named attrs that holds slog.Attr value (later more than this single value)
	attrs := []slog.Attr{
		{Key: "message", Value: slog.StringValue(err.Error())},
	}
	// ... "spreads" slice, so each element is passed as an individual argument to append
	attrs = append(attrs, linkoerr.Attrs(err)...)
	// linkoerr.Attrs(err) calls your helper function, which walks the error chain and
	// returns a []slog.Attr - all the extra structured fields (like "path", "item_no", etc.)
	//  that were attached anywhere in the chain via WithAttrs
	// 	(example in 'store.go' walk():  ch <- ShortURL{Err: linkoerr.WithAttrs(err, "path", filepath.Join(s.dir, e.Name()))}

	// else-case: stack-trace was successfully created, then add it.
	if stackErr, ok := errors.AsType[stackTracer](err); ok {
		attrs = append(attrs, slog.Attr{
			Key:   "stack_trace",
			Value: slog.StringValue(fmt.Sprintf("%+v", stackErr.StackTrace())),
		})
	}
	// each element from slice 'attrs' is added individually by using 'attrs...'
	return slog.GroupAttrs("error", attrs...)
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
