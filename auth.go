package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	pkgerr "github.com/pkg/errors"
	"github.com/zeelna/linko-logging/internal/logger"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const UserContextKey contextKey = "user"

var allowedUsers = map[string]string{
	"frodo":   "$2a$10$B6O/n6teuCzpuh66jrUAdeaJ3WvXcxRkzpN0x7H.di9G9e/NGb9Me",
	"samwise": "$2a$10$EWZpvYhUJtJcEMmm/IBOsOGIcpxUnGIVMRiDlN/nxl1RRwWGkJtty",
	// frodo: "ofTheNineFingers"
	// samwise: "theStrong"
	"saruman": "invalidFormat",
}

func (s *server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		stored, exists := allowedUsers[username]
		if !exists {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		ok, err := s.validatePassword(password, stored)
		if err != nil {
			s.logger.Error("error validating password",
				slog.String("user", username),
				slog.Any("error", err),
			)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), UserContextKey, username))

		// read the *LogContext that requestLogger stored:
		logContextVal := r.Context().Value(logger.LogContextKey)
		logContext, ok := logContextVal.(*logger.LogContext)
		if !ok || logContext == nil {
			s.logger.Error("no logContext for auth middleware",
				slog.String("path", r.URL.Path),
				slog.Any("value_type", fmt.Sprintf("%T", logContextVal)),
			)

			next.ServeHTTP(w, r)
			return
		}
		logContext.Username = username
		next.ServeHTTP(w, r)
	})
}

func (s *server) validatePassword(password, stored string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(password))
	if err == bcrypt.ErrMismatchedHashAndPassword {
		return false, nil
	}
	if err != nil {
		// LOGGING: Create error with Stack Trace. Logging delegated to outer fn (authMiddleware)
		err := pkgerr.WithStack(err)
		return false, err
	}
	return true, nil
}
