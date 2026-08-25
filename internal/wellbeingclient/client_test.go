package wellbeingclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient wires a Client to an httptest server with retries disabled and
// fast polling, so tests neither sleep nor retry unless they opt in.
func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, "test-token", "1000",
		WithMaxRetries(0),
		WithPollIntervals(time.Millisecond, time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	return c, srv
}

func TestNewClientRequiresHostTokenAndCompany(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		host      string
		token     string
		companyID string
	}{
		{name: "missing host", token: "t", companyID: "1000"},
		{name: "missing token", host: "https://example.test", companyID: "1000"},
		{name: "missing company", host: "https://example.test", token: "t"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewClient(tt.host, tt.token, tt.companyID); err == nil {
				t.Error("NewClient returned nil error, want error")
			}
		})
	}
}

func TestDoSendsBearerTokenAndJoinsTrailingSlashHost(t *testing.T) {
	t.Parallel()

	var gotAuth, gotPath, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	// Trailing slash on the host must not produce a doubled slash in the path.
	c, err := NewClient(srv.URL+"/", "test-token", "1000", WithMaxRetries(0))
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	var out map[string]any
	status, err := c.do(context.Background(), http.MethodGet, "/v1.0/Company/1000/Employee", nil, &out)
	if err != nil {
		t.Fatalf("do returned error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-token")
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if gotPath != "/v1.0/Company/1000/Employee" {
		t.Errorf("path = %q, want %q", gotPath, "/v1.0/Company/1000/Employee")
	}
}

func TestDoEncodesRequestBodyAsJSON(t *testing.T) {
	t.Parallel()

	var gotBody, gotContentType string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{}`))
	}))

	in := map[string]string{"Firstname": "Bilbo"}
	if _, err := c.do(context.Background(), http.MethodPut, "/x", in, nil); err != nil {
		t.Fatalf("do returned error: %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if !strings.Contains(gotBody, `"Firstname":"Bilbo"`) {
		t.Errorf("body = %q, want it to contain the encoded field", gotBody)
	}
}

func TestDoReturnsAPIErrorOnUnauthorized(t *testing.T) {
	t.Parallel()

	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"Message":"Authorization has been denied for this request."}`))
	}))

	_, err := c.do(context.Background(), http.MethodGet, "/x", nil, nil)
	if err == nil {
		t.Fatal("do returned nil error, want error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
}

func TestDoRetriesServerErrors(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, "t", "1000", WithMaxRetries(3))
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	if _, err := c.do(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatalf("do returned error after retries: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls = %d, want 3", got)
	}
}

func TestDoDoesNotRetryClientErrors(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ValidationErrors":["nope"]}`))
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(srv.URL, "t", "1000", WithMaxRetries(3))

	if _, err := c.do(context.Background(), http.MethodGet, "/x", nil, nil); err == nil {
		t.Fatal("do returned nil error, want error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls = %d, want 1 — 4xx must not be retried", got)
	}
}

func TestDoHonoursContextCancellation(t *testing.T) {
	t.Parallel()

	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := c.do(ctx, http.MethodGet, "/x", nil, nil); err == nil {
		t.Fatal("do returned nil error, want context deadline error")
	}
}
