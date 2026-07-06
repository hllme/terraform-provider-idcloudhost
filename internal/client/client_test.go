package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fastRetryOpts keeps retry tests from actually waiting seconds.
func fastRetryOpts(server *httptest.Server) []Option {
	return []Option{
		WithHTTPClient(server.Client()),
		WithBaseURL(server.URL),
		WithRetryPolicy(2, time.Millisecond),
	}
}

func TestGetVM_ErrorsInsideOKBody(t *testing.T) {
	// The sharpest API edge: a 200 status with an `errors` key in the body
	// must be treated as a failure, not decoded as a successful VM.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors": {"uuid": "not found"}}`))
	}))
	defer server.Close()

	c := New("test-key", fastRetryOpts(server)...)
	_, err := c.GetVM(context.Background(), "some-uuid")
	if err == nil {
		t.Fatal("expected an error when the body contains an errors key, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 to be preserved on the APIError, got %d", apiErr.StatusCode)
	}
}

func TestGetVM_NullErrorsKeyIsSuccess(t *testing.T) {
	// A literal `"errors": null` (or absent) must NOT be treated as a failure.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors": null, "uuid": "vm-1", "status": "running"}`))
	}))
	defer server.Close()

	c := New("test-key", fastRetryOpts(server)...)
	vm, err := c.GetVM(context.Background(), "vm-1")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if vm.UUID != "vm-1" || vm.Status != "running" {
		t.Fatalf("unexpected VM decoded: %+v", vm)
	}
}

func TestGetVM_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message": "not found"}`))
	}))
	defer server.Close()

	c := New("test-key", fastRetryOpts(server)...)
	_, err := c.GetVM(context.Background(), "missing-uuid")
	if err == nil {
		t.Fatal("expected an error for 404")
	}
	if !NotFound(err) {
		t.Fatalf("expected NotFound(err) to be true, got err: %v", err)
	}
}

func TestDoJSON_RetriesOn429ThenSucceeds(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message": "rate limited"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"uuid": "vm-1", "status": "running"}`))
	}))
	defer server.Close()

	c := New("test-key", fastRetryOpts(server)...)
	vm, err := c.GetVM(context.Background(), "vm-1")
	if err != nil {
		t.Fatalf("expected eventual success after retries, got: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	if vm.UUID != "vm-1" {
		t.Fatalf("unexpected VM: %+v", vm)
	}
}

func TestDoJSON_RetriesOn5xxExhausted(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message": "boom"}`))
	}))
	defer server.Close()

	c := New("test-key", fastRetryOpts(server)...)
	_, err := c.GetVM(context.Background(), "vm-1")
	if err == nil {
		t.Fatal("expected an error once retries are exhausted")
	}
	if attempts != 3 { // maxRetries=2 => 1 initial + 2 retries
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDoJSON_DoesNotRetryOn4xx(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message": "bad request"}`))
	}))
	defer server.Close()

	c := New("test-key", fastRetryOpts(server)...)
	_, err := c.GetVM(context.Background(), "vm-1")
	if err == nil {
		t.Fatal("expected an error for 400")
	}
	if attempts != 1 {
		t.Fatalf("expected exactly 1 attempt (no retry on 4xx), got %d", attempts)
	}
}

func TestCreateVM_SendsAPIKeyHeaderAndBody(t *testing.T) {
	var gotHeader, gotContentType string
	var gotName string
	var gotBillingAccountID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("apikey")
		gotContentType = r.Header.Get("Content-Type")
		_ = r.ParseForm()
		gotName = r.PostForm.Get("name")
		gotBillingAccountID = r.PostForm.Get("billing_account_id")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"uuid": "vm-new", "status": "queued"}`))
	}))
	defer server.Close()

	c := New("super-secret-key", fastRetryOpts(server)...)
	vm, err := c.CreateVM(context.Background(), CreateVMInput{
		Name:             "test-vm",
		BillingAccountID: 42,
		OSName:           "ubuntu",
		OSVersion:        "22.04-lts",
		VCPU:             1,
		RAM:              1024,
		Disks:            20,
		Username:         "ops",
		Password:         "S3cretPass1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotHeader != "super-secret-key" {
		t.Fatalf("expected apikey header to be sent, got %q", gotHeader)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("expected form-urlencoded content type, got %q", gotContentType)
	}
	if gotName != "test-vm" || gotBillingAccountID != "42" {
		t.Fatalf("unexpected form fields decoded: name=%q billing_account_id=%q", gotName, gotBillingAccountID)
	}
	if vm.UUID != "vm-new" {
		t.Fatalf("unexpected VM: %+v", vm)
	}
}

func TestDeleteVM_NotFoundIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message": "not found"}`))
	}))
	defer server.Close()

	c := New("test-key", fastRetryOpts(server)...)
	err := c.DeleteVM(context.Background(), "already-gone")
	if err == nil || !NotFound(err) {
		t.Fatalf("expected a NotFound error, got: %v", err)
	}
}

func TestDoJSON_ContextCancellationStopsRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	c := New("test-key", WithHTTPClient(server.Client()), WithBaseURL(server.URL), WithRetryPolicy(10, 50*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.GetVM(ctx, "vm-1")
	if err == nil {
		t.Fatal("expected an error due to context cancellation")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("expected retries to stop promptly on context cancellation, took %v", time.Since(start))
	}
}
