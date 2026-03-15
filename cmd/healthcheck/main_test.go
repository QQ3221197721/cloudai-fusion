package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRun_NoArgs(t *testing.T) {
	code := run([]string{})
	if code != 2 {
		t.Errorf("expected exit code 2 for no args, got %d", code)
	}
}

func TestRun_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	}))
	defer srv.Close()

	code := run([]string{srv.URL})
	if code != 0 {
		t.Errorf("expected exit code 0 for healthy endpoint, got %d", code)
	}
}

func TestRun_Unhealthy_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	code := run([]string{srv.URL})
	if code != 1 {
		t.Errorf("expected exit code 1 for 500 response, got %d", code)
	}
}

func TestRun_Unhealthy_503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	code := run([]string{srv.URL})
	if code != 1 {
		t.Errorf("expected exit code 1 for 503 response, got %d", code)
	}
}

func TestRun_ConnectionRefused(t *testing.T) {
	// Use a URL that will definitely fail to connect
	code := run([]string{"http://127.0.0.1:1", "1"})
	if code != 1 {
		t.Errorf("expected exit code 1 for connection refused, got %d", code)
	}
}

func TestRun_CustomTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	code := run([]string{srv.URL, "10"})
	if code != 0 {
		t.Errorf("expected exit code 0 with custom timeout, got %d", code)
	}
}

func TestRun_InvalidTimeout(t *testing.T) {
	// Invalid timeout string should be ignored, default 5s used
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	code := run([]string{srv.URL, "notanumber"})
	if code != 0 {
		t.Errorf("expected exit code 0 with invalid timeout (default used), got %d", code)
	}
}

func TestRun_ZeroTimeout(t *testing.T) {
	// Zero timeout should be ignored, default 5s used
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	code := run([]string{srv.URL, "0"})
	if code != 0 {
		t.Errorf("expected exit code 0 with zero timeout (default used), got %d", code)
	}
}

func TestRun_StatusCodes(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		wantCode int
	}{
		{"200 OK", http.StatusOK, 0},
		{"201 Created", http.StatusCreated, 0},
		{"204 No Content", http.StatusNoContent, 0},
		{"301 Redirect", http.StatusMovedPermanently, 0},
		{"400 Bad Request", http.StatusBadRequest, 1},
		{"401 Unauthorized", http.StatusUnauthorized, 1},
		{"403 Forbidden", http.StatusForbidden, 1},
		{"404 Not Found", http.StatusNotFound, 1},
		{"500 Internal Server Error", http.StatusInternalServerError, 1},
		{"502 Bad Gateway", http.StatusBadGateway, 1},
		{"503 Service Unavailable", http.StatusServiceUnavailable, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			code := run([]string{srv.URL})
			if code != tt.wantCode {
				t.Errorf("status %d: expected exit code %d, got %d", tt.status, tt.wantCode, code)
			}
		})
	}
}
