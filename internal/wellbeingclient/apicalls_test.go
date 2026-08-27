package wellbeingclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// apiCallsHandler serves a scripted sequence of ApiCalls responses, one per
// request, repeating the last entry once the script is exhausted.
func apiCallsHandler(t *testing.T, bodies []string, calls *atomic.Int32) http.Handler {
	t.Helper()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/Company/1000/ApiCalls" {
			t.Errorf("path = %q, want /v1.0/Company/1000/ApiCalls", r.URL.Path)
		}
		i := int(calls.Add(1)) - 1
		if i >= len(bodies) {
			i = len(bodies) - 1
		}
		_, _ = w.Write([]byte(bodies[i]))
	})
}

func apiCallBody(operationID string, statusCode, reason string) string {
	return fmt.Sprintf(`[{"ApiOperationId":%q,"UserId":7,"Username":"api","CreatedOn":"2026-08-25T10:00:00","Method":"PUT","HttpStatusCode":%s,"HttpStatusReason":%q}]`,
		operationID, statusCode, reason)
}

func TestWaitForOperationSucceedsAfterProcessing(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	bodies := []string{
		apiCallBody("op-1", `""`, ""),               // created, not yet validated
		apiCallBody("op-1", `202`, "Accepted"),      // queued
		apiCallBody("op-1", `202`, "Processing..."), // running
		apiCallBody("op-1", `200`, "OK"),            // done
	}
	c, _ := newTestClient(t, apiCallsHandler(t, bodies, &calls))

	if err := c.WaitForOperation(context.Background(), "op-1"); err != nil {
		t.Fatalf("WaitForOperation returned error: %v", err)
	}
	if got := calls.Load(); got != 4 {
		t.Errorf("polls = %d, want 4", got)
	}
}

func TestWaitForOperationFailsOnClientError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	bodies := []string{
		apiCallBody("op-2", `202`, "Accepted"),
		apiCallBody("op-2", `400`, "Batch limit exceeded"),
	}
	c, _ := newTestClient(t, apiCallsHandler(t, bodies, &calls))

	err := c.WaitForOperation(context.Background(), "op-2")
	if err == nil {
		t.Fatal("WaitForOperation returned nil error, want error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *APIError", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Message != "Batch limit exceeded" {
		t.Errorf("Message = %q, want the HttpStatusReason", apiErr.Message)
	}
	if apiErr.ApiOperationID != "op-2" {
		t.Errorf("ApiOperationID = %q, want op-2", apiErr.ApiOperationID)
	}
}

func TestWaitForOperationFailsOnServerError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c, _ := newTestClient(t, apiCallsHandler(t, []string{apiCallBody("op-3", `500`, "Unexpected failure")}, &calls))

	err := c.WaitForOperation(context.Background(), "op-3")
	if err == nil {
		t.Fatal("WaitForOperation returned nil error, want error")
	}
}

func TestWaitForOperationKeepsWaitingWhenOperationNotYetListed(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	bodies := []string{
		`[]`, // operation has not appeared in the list yet
		apiCallBody("op-4", `200`, "OK"),
	}
	c, _ := newTestClient(t, apiCallsHandler(t, bodies, &calls))

	if err := c.WaitForOperation(context.Background(), "op-4"); err != nil {
		t.Fatalf("WaitForOperation returned error: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("polls = %d, want 2", got)
	}
}

func TestWaitForOperationRespectsContextDeadline(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c, _ := newTestClient(t, apiCallsHandler(t, []string{apiCallBody("op-5", `202`, "Processing...")}, &calls))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := c.WaitForOperation(ctx, "op-5")
	if err == nil {
		t.Fatal("WaitForOperation returned nil error, want timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want it to wrap context.DeadlineExceeded", err)
	}
}

func TestGetAPICallReturnsNilWhenAbsent(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c, _ := newTestClient(t, apiCallsHandler(t, []string{`[]`}, &calls))

	got, err := c.GetAPICall(context.Background(), "missing")
	if err != nil {
		t.Fatalf("GetAPICall returned error: %v", err)
	}
	if got != nil {
		t.Errorf("GetAPICall = %+v, want nil", got)
	}
}
