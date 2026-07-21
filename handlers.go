package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/zeelna/linko-logging/internal/linkoerr"
	"github.com/zeelna/linko-logging/internal/logger"
	"github.com/zeelna/linko-logging/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const shortURLLen = len("http://localhost:8080/") + 6

var (
	redirectsMu sync.Mutex
	redirects   []string
)

//go:embed index.html
var indexPage string

func (s *server) handlerIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	io.WriteString(w, indexPage)
}

func (s *server) handlerLogin(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *server) handlerShortenLink(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(UserContextKey).(string)
	if !ok || user == "" {
		logger.HttpError(r.Context(), w, http.StatusUnauthorized, errors.New("unauthorized"))
		return
	}
	longURL := r.FormValue("url")
	if longURL == "" {
		//http.Error(w, "missing url parameter", http.StatusBadRequest)
		logger.HttpError(r.Context(), w, http.StatusBadRequest, errors.New("missing url parameter"))
		return
	}
	u, err := url.Parse(longURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		clean := errors.New("invalid URL")
		reason := "url must include scheme (http/https) and host"
		logged := linkoerr.WithAttrs(clean, "url", longURL, "reason", reason) // detail kept as a field
		logger.HttpError(r.Context(), w, http.StatusBadRequest, logged)
		return
	}

	if err := checkDestination(longURL); err != nil {
		// http.Error(w, fmt.Sprintf("invalid target URL: %v", err), http.StatusBadRequest)
		clean := errors.New("invalid target url")
		logged := linkoerr.WithAttrs(clean, "reason", err.Error(), "url", longURL) // error.url=<invalid_url>, error.reason=<long-format>
		logger.HttpError(r.Context(), w, http.StatusBadRequest, logged)
		return
	}
	shortCode, err := s.store.Create(r.Context(), longURL)
	if err != nil {
		clean := errors.New("failed to shorten url")
		logged := linkoerr.WithAttrs(clean, "reason", err.Error(), "url", longURL) // error.url=<invalid_url>, error.reason=<long-format>
		logger.HttpError(r.Context(), w, http.StatusInternalServerError, logged)
		return
	}
	// Log message.
	s.logger.Info("Successfully generated short code",
		slog.String("shortCode", shortCode),
		slog.String("longUrl", longURL),
	)

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, shortCode)
}

func (s *server) handlerRedirect(w http.ResponseWriter, r *http.Request) {
	longURL, err := s.store.Lookup(r.Context(), r.PathValue("shortCode"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			//http.Error(w, "not found", http.StatusNotFound)
			logger.HttpError(r.Context(), w, http.StatusNotFound, errors.New("not found"))
		} else {
			// Structured log.
			s.logger.Error("failed to lookup URL",
				slog.Any("error", err),
			)
			//http.Error(w, "internal server error", http.StatusInternalServerError)
			//err = fmt.Errorf("internal server error: %w", err) // with stack trace.
			err = errors.New("internal server error") // without stack trace.
			logger.HttpError(r.Context(), w, http.StatusInternalServerError, err)
		}
		return
	}
	_, _ = bcrypt.GenerateFromPassword([]byte(longURL), bcrypt.DefaultCost)
	if err := checkDestination(longURL); err != nil {
		clean := errors.New("destination unavailable")
		logged := linkoerr.WithAttrs(clean, "reason", err.Error(), "url", longURL) // error.url=<invalid_url>, error.reason=<long-format>
		logger.HttpError(r.Context(), w, http.StatusBadGateway, logged)
		return
	}

	redirectsMu.Lock()
	redirects = append(redirects, strings.Repeat(longURL, 1024))
	redirectsMu.Unlock()

	http.Redirect(w, r, longURL, http.StatusFound)
}

func (s *server) handlerListURLs(w http.ResponseWriter, r *http.Request) {
	codes, err := s.store.List(r.Context())
	if err != nil {
		s.logger.Error("failed to list URLs", slog.Any("error", err))
		//http.Error(w, "failed to list URLs", http.StatusInternalServerError)

		err = fmt.Errorf("failed to list URLs: %v", err)
		logger.HttpError(r.Context(), w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(codes)
}

func (s *server) handlerStats(w http.ResponseWriter, _ *http.Request) {
	redirectsMu.Lock()
	snapshot := redirects
	redirectsMu.Unlock()

	var bytesSaved int
	for _, u := range snapshot {
		bytesSaved += len(u) - shortURLLen
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{
		"redirects":   len(snapshot),
		"bytes_saved": bytesSaved,
	})
}
