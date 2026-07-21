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

// Buffered Writer must be flushed before the program exits.
type CloseFunc func() error

// Helper to set the destination(s) of the all log entries based on whether 'LINKO_LOG_FILE' environment variable is set.
func InitializeLogger(logFileEnv string) (*slog.Logger, CloseFunc, error) {

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
		var closeFn CloseFunc = func() error { return nil }
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
	var closeFn CloseFunc = func() error {
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

// ###################################################################################################################
// -- Logging errors with attributes (errors.error_1.message, errors.error_1.path, errors.error_2.message, etc) --
// stackTracer interface to extract stack traces from errors wrapped with pkg/errors:
type stackTracer interface {
	error
	StackTrace() pkgerr.StackTrace
}

type multiError interface {
	error
	Unwrap() []error
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
	err, ok := a.Value.Resolve().Any().(error)
	if !ok {
		return a
	}
	// Unveil whether multiple errors or a single err
	multiErr, ok := errors.AsType[multiError](err)
	// single-error case:
	if !ok { // will be "error" key
		singleErrAttrs := errorAttrs(err)
		return slog.GroupAttrs("error", singleErrAttrs...) // // each element from slice 'attrs' is added individually by using 'attrs...'
	}

	// multi-error case: // will be "errors" key
	var multiErrGroups []slog.Attr
	for i, childError := range multiErr.Unwrap() {
		// Multiple errors structured and grouped by their index+1.
		singleErrAttrs := errorAttrs(childError)                                          // <-- helper fn called to create the error's attributes
		numberedGroup := slog.GroupAttrs(fmt.Sprintf("error_%d", i+1), singleErrAttrs...) // error_2.path, error_2.message,
		multiErrGroups = append(multiErrGroups, numberedGroup)                            // conceptually: [error_1.path, error_1.message] + error_2.path + error_2.message,
	}
	// each element from slice 'attrs' is added individually by using 'slog.Attr...' ( here: multiErrGroups... )
	return slog.GroupAttrs("errors", multiErrGroups...) // Uses the "errors" outer group
}

// Helper function to create Error's attributes. Required to create a slog.Group by calling slog.GroupAttrs("error_i", attrs)
func errorAttrs(err error) []slog.Attr {
	attrs := []slog.Attr{
		{Key: "message", Value: slog.StringValue(err.Error())},
	}
	attrs = append(attrs, linkoerr.Attrs(err)...)
	// If stack trace could be found, add that as well:
	if stackErr, ok := errors.AsType[stackTracer](err); ok {
		attrs = append(attrs, slog.Attr{
			Key:   "stack_trace",
			Value: slog.StringValue(fmt.Sprintf("%+v", stackErr.StackTrace())),
		})
	}
	return attrs
}

// ###################################################################################################################
