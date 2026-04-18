package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestRequestID(t *testing.T) {
	// Empty context
	ctx := context.Background()
	if id := RequestID(ctx); id != "" {
		t.Errorf("expected empty string for context without request ID, got '%s'", id)
	}

	// Context with request ID
	ctx = context.WithValue(ctx, requestIDKey, "abc123")
	if id := RequestID(ctx); id != "abc123" {
		t.Errorf("expected 'abc123', got '%s'", id)
	}
}

func TestRequestIDMiddleware_GeneratesID(t *testing.T) {
	var capturedID string

	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = RequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if capturedID == "" {
		t.Error("expected request ID to be generated")
	}
	if len(capturedID) != 8 {
		t.Errorf("expected 8 character ID, got %d characters: '%s'", len(capturedID), capturedID)
	}

	// Check response header
	if rr.Header().Get("X-Request-ID") != capturedID {
		t.Error("expected X-Request-ID header to be set")
	}
}

func TestRequestIDMiddleware_UsesProvidedID(t *testing.T) {
	var capturedID string

	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = RequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "custom-id-123")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if capturedID != "custom-id-123" {
		t.Errorf("expected 'custom-id-123', got '%s'", capturedID)
	}
}

func TestLoggingMiddleware(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handler := LoggingMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/api2/json/nodes/pve/qemu", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", rr.Code)
	}
}

func TestResponseWriter_CapturesStatusCode(t *testing.T) {
	rr := httptest.NewRecorder()
	wrapped := &responseWriter{ResponseWriter: rr, statusCode: http.StatusOK}

	wrapped.WriteHeader(http.StatusNotFound)

	if wrapped.statusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", wrapped.statusCode)
	}
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected underlying recorder to have status 404, got %d", rr.Code)
	}
}
