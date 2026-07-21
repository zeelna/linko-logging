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
			logger.HttpError(r.Context(), w, http.StatusUnauthorized, fmt.Errorf("unauthorized"))
			return
		}
		stored, exists := allowedUsers[username]
		if !exists {
			logger.HttpError(r.Context(), w, http.StatusUnauthorized, fmt.Errorf("unauthorized"))
			return
		}
		ok, err := s.validatePassword(password, stored)
		if err != nil {
			s.logger.Error("error validating password",
				slog.String("user", username),
				slog.Any("error", err),
			)


			logger.HttpError(r.Context(), w, http.StatusInternalServerError,
				fmt.Errorf("internal server error"))
			return
		}
		if !ok {
			logger.HttpError(r.Context(), w, http.StatusUnauthorized, fmt.Errorf("unauthorized"))
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), UserContextKey, username))

		// Get the *LogContext object via LogContextKey. (Should not be creating anything new, a 'getter' instead)
		logContext, _ := r.Context().Value(logger.LogContextKey).(*logger.LogContext)
		if logContext != nil {
			logContext.Username = username
		}
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
