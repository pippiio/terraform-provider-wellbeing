package wellbeingclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListEmployees(t *testing.T) {
	t.Parallel()

	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1.0/Company/1000/Employee" {
			t.Errorf("path = %q, want /v1.0/Company/1000/Employee", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[` + employeeGETFixture + `]`))
	}))

	got, err := c.ListEmployees(context.Background())
	if err != nil {
		t.Fatalf("ListEmployees returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].EmployeeID != "12345678" {
		t.Errorf("EmployeeID = %q, want %q", got[0].EmployeeID, "12345678")
	}
}

func TestListEmployeesEmptyRoster(t *testing.T) {
	t.Parallel()

	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))

	got, err := c.ListEmployees(context.Background())
	if err != nil {
		t.Fatalf("ListEmployees returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestReplaceEmployeesSynchronous(t *testing.T) {
	t.Parallel()

	var gotPayload []Employee
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("decoding request payload: %v", err)
		}
		_, _ = w.Write([]byte(`{"ApiOperationId":"op-1","Inserted":1,"Updated":0,"Removed":2,"WasQueued":"false"}`))
	}))

	in := []Employee{{EmployeeID: "a", Firstname: "Bilbo", Lastname: "Baggins", Email: "b@shire.test"}}
	got, err := c.ReplaceEmployees(context.Background(), in)
	if err != nil {
		t.Fatalf("ReplaceEmployees returned error: %v", err)
	}
	if got.WasQueued.Bool() {
		t.Error("WasQueued = true, want false for a 200 response")
	}
	if got.Inserted != 1 || got.Removed != 2 {
		t.Errorf("Inserted/Removed = %d/%d, want 1/2", got.Inserted, got.Removed)
	}
	if len(gotPayload) != 1 || gotPayload[0].EmployeeID != "a" {
		t.Errorf("payload = %+v, want one employee with EmployeeID a", gotPayload)
	}
}

func TestReplaceEmployeesQueued(t *testing.T) {
	t.Parallel()

	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		// Deliberately reports WasQueued false; the 202 status must win.
		_, _ = w.Write([]byte(`{"ApiOperationId":"op-9","WasQueued":"false"}`))
	}))

	got, err := c.ReplaceEmployees(context.Background(), nil)
	if err != nil {
		t.Fatalf("ReplaceEmployees returned error: %v", err)
	}
	if !got.WasQueued.Bool() {
		t.Error("WasQueued = false, want true — HTTP 202 is authoritative")
	}
	if got.ApiOperationID != "op-9" {
		t.Errorf("ApiOperationID = %q, want op-9", got.ApiOperationID)
	}
}

func TestReplaceEmployeesSendsEmptyArrayNotNull(t *testing.T) {
	t.Parallel()

	var raw json.RawMessage
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		_, _ = w.Write([]byte(`{"ApiOperationId":"op-1"}`))
	}))

	if _, err := c.ReplaceEmployees(context.Background(), nil); err != nil {
		t.Fatalf("ReplaceEmployees returned error: %v", err)
	}
	if string(raw) != "[]" {
		t.Errorf("payload = %s, want [] — a null body would be rejected", raw)
	}
}

func TestReplaceEmployeesValidationError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ApiOperationId":"op-7","ValidationErrors":["Email is not unique"]}`))
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(srv.URL, "t", "1000", WithMaxRetries(0))

	_, err := c.ReplaceEmployees(context.Background(), nil)
	if err == nil {
		t.Fatal("ReplaceEmployees returned nil error, want error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *APIError", err)
	}
	if len(apiErr.ValidationErrors) != 1 {
		t.Errorf("ValidationErrors = %v, want one entry", apiErr.ValidationErrors)
	}
	if apiErr.ApiOperationID != "op-7" {
		t.Errorf("ApiOperationID = %q, want op-7", apiErr.ApiOperationID)
	}
}
