package logger

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"github.com/natefinch/lumberjack"
	pkgerr "github.com/pkg/errors"
	"github.com/zeelna/linko-logging/internal/linkoerr"
)

// Middleware to read / generate X-Request-ID from inbound request before logger.RequestLogger() call
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if there is 'X-Request-ID' in the HTTP Request Header.
		requestID := r.Header.Get("X-Request-ID")
		// If not, create a cryptographically random string, and assign it
		if requestID == "" {
			requestID = rand.Text()
			r.Header.Set("X-Request-ID", requestID)
		}
		// Assign this requestID in the Response (HTTP) Header.
		w.Header().Set("X-Request-ID", requestID) // Downstream Logger will read from this .Get()
		next.ServeHTTP(w, r)
	}) // Propagate request IDs through headers and logs through HTTP Headers (response).
} // Update your server-wide handler to use the new middleware. It should be called before the request logger middleware
// see: srv := &http.Server{ ..., Handler: logger.RequestIDMiddleware(logger.RequestLogger(myLogger)(mux)), }

// Middleware to log with Dependency Injected logger, stored in s.standardLogger
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ----------------------------------------------------
			// Include authenticated username in the request logs
			logCtx := &LogContext{}
			r = r.WithContext(context.WithValue(r.Context(), LogContextKey, logCtx))
			// ---------------------------------------------------

			// Request Metadata logging:
			spyReader := &spyReadCloser{ReadCloser: r.Body}
			r.Body = spyReader

			// Response Metadata logging (wrapping 'w', and passing in .ServeHTTP):
			spyWriter := &spyResponseWriter{ResponseWriter: w}

			// Record request start time, then subtract when the response finishes.
			start := time.Now() // starting clock
			next.ServeHTTP(spyWriter, r)
			//  --- Logging HTTP Response and Request attributes ---
			attrs := []any{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("request_id", spyWriter.Header().Get("X-Request-ID")),
				slog.String("client_ip", redactIP(r.RemoteAddr)),
				slog.Duration("duration", time.Since(start)), // time.Since() calculates the duration
				//slog.String("user", logCtx.Username),
				slog.Int("request_body_bytes", spyReader.bytesRead),
				slog.Int("response_status", spyWriter.statusCode),
				slog.Int("response_body_bytes", spyWriter.bytesWritten),
				slog.Any("error", logCtx.Error),
			}
			if logCtx.Username != "" {
				attrs = append(attrs, slog.String("user", logCtx.Username)) // populated by authMiddleware
			}
			logger.Info("Served request", attrs...)
			// --------------------------------------------------
		})
	}
}

// ---------------------------
// Request Metadata Logging
// ---------------------------
type spyReadCloser struct {
	io.ReadCloser
	bytesRead int
}

func (r *spyReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.bytesRead += n
	return n, err
}

// ---------------------------

// ---------------------------
// Response Metadata Logging
// ---------------------------
type spyResponseWriter struct {
	http.ResponseWriter
	bytesWritten int
	statusCode   int
}

func (w *spyResponseWriter) Write(p []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytesWritten += n
	return n, err
}
func (w *spyResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// ---------------------------
// Logging user Context
type contextKey string

const LogContextKey contextKey = "log_context"

type LogContext struct {
	Username string
	Error    error
}

// helperfn  that wraps http.Error. It still sends the HTTP response, but first stores the error in LogContext (if present) so request logs can include it
func HttpError(ctx context.Context, w http.ResponseWriter, status int, err error) {
	if logCtx, ok := ctx.Value(LogContextKey).(*LogContext); ok {
		logCtx.Error = err
	}
	// Sanitize all error's content which match to HTTP Status Code 401 (Unauthorized), 403 (Forbidden) and 500 (Internal Server Error)
	isLeakyError := status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusInternalServerError
	if isLeakyError {
		http.Error(w, http.StatusText(status), status)
	} else {
		http.Error(w, err.Error(), status)
	}
}

// -------------------------

// Buffered Writer must be flushed before the program exits.
type CloseFunc func() error

// Helper to set the destination(s) of the all log entries based on whether 'LINKO_LOG_FILE' environment variable is set.
func InitializeLogger(logFileEnv string) (*slog.Logger, CloseFunc, error) {
	//func InitializeLogger(logFileEnv string) (*lumberjack.Logger, CloseFunc, error) {
	/*
		// A more practical approach is a single logger that routes logs to different destinations by level.
		//For example, everything goes to STDERR, but only INFO and higher go to a file.
		// Assume that in production, Linko has a LINKO_LOG_FILE environment variable set.
		// If LINKO_LOG_FILE environment variable is not set, the logger only write to STDERR.
	*/
	if logFileEnv == "" {
		// A. log.DEBUG. Create single logger with different destinations by level^
		isTerminal := isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())
		debugHandler := tint.NewTextHandler(os.Stderr, &tint.Options{
			NoColor: !isTerminal, // deliberate ! (negation)
		})
		logger := slog.New(debugHandler)
		// Create a non-operational function of type closeFunc due to no bufio.BufferedWriter, and as such, no need to .Flush()
		var closeFn CloseFunc = func() error { return nil }
		return logger, closeFn, nil
	}
	// Otherwise if LINKO_LOG_FILE is set, it should write both to file and STDERR.
	//  %%% %%% %%% B. log.Info (into file).  %%% %%% %%%
	/*
		file, err := os.OpenFile(logFileEnv, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, err
		}
	*/
	/* Logs are written to disk, no matter how large or small the message is.
		- That's potentially a lot of disk I/O, and it can really slow down our entire application
	 	+ use a buffered writer like bufio.Writer around the file.
		// This allows us to write log messages to an in-memory buffer,
		// and then that buffer is only written to disk when it's full.
	*/
	logWriter := &lumberjack.Logger{
		Filename:   logFileEnv,
		MaxSize:    1,
		MaxAge:     28,
		MaxBackups: 10,
		LocalTime:  false,
		Compress:   true,
	}
	//const bufferedBytes = 8192
	//bufferedFileWriter := bufio.NewWriterSize(file, 8192) // buffered bytes, 8192
	//infoHandler := slog.NewJSONHandler(bufferedFileWriter, &slog.HandlerOptions{

	infoHandler := slog.NewJSONHandler(logWriter, &slog.HandlerOptions{
		Level:       slog.LevelInfo, // ^ Debug and above into FILE ^
		ReplaceAttr: replaceAttr,    // any logged error is accompanied by stack trace + error
	})
	_isTerminal := isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())
	debugHandler := tint.NewTextHandler(os.Stderr, &tint.Options{
		Level:   slog.LevelDebug,
		NoColor: !_isTerminal, // negation purposefully.
	})

	myLogger := slog.New(slog.NewMultiHandler(
		debugHandler,
		infoHandler,
	)) // DEBUG and above: into os.Stderr, // INFO and above: into bufferedFile (linko.access.log)

	// ----- Safe cleanup (fn-expression) to free resources before program exits ------------
	// Function expression to clear *bufio.Writer and close *File before program exits.
	/*
		var closeFn CloseFunc = func() error {
			if err := bufferedFileWriter.Flush(); err != nil { return err }
			if err := file.Close(); err != nil { return err }
			return nil //  <- Happy path of function expression named closeFn
		}
		return logger, closeFn, nil
	*/
	// -------------------------------------------------------------------------------------
	closeFn := func() error { return logWriter.Close() } // lumberjack.logger's .Close() function.
	return myLogger, closeFn, nil
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
// helper function to redact final octet of the IP address (hack solution: in reality host-portion can be more than 1 octet)
func redactIP(address string) string {
	// if any errors parsing / splitting, return those erroneous IP (or IPv6 or others) as-is, for debugging purposes
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return address
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return address
	}
	// We are excluding the port number if the IPv4 address was correct.
	return fmt.Sprintf("%d.%d.%d.%s", ip4[0], ip4[1], ip4[2], "x") // change 192.168.1.4 to 192.168.1.x
}
