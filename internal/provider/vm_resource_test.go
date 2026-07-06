package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hllme/terraform-provider-idcloudhost/internal/client"
)

func newTestVMResource(t *testing.T, handler http.HandlerFunc) *vmResource {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	oldInterval := pollInterval
	pollInterval = time.Millisecond
	t.Cleanup(func() { pollInterval = oldInterval })

	return &vmResource{
		client: client.New("test-key",
			client.WithHTTPClient(server.Client()),
			client.WithBaseURL(server.URL),
			client.WithRetryPolicy(1, time.Millisecond),
		),
	}
}

func TestPollUntilStatus_WaitsThroughTransitionalStates(t *testing.T) {
	var calls int32
	r := newTestVMResource(t, func(w http.ResponseWriter, req *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		status := "queued"
		switch {
		case n >= 4:
			status = "running"
		case n >= 2:
			status = "building"
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"uuid": "vm-1", "status": status})
	})

	vm, err := r.pollUntilStatus(context.Background(), "vm-1", vmStatusRunning)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vm.Status != vmStatusRunning {
		t.Fatalf("expected final status running, got %q", vm.Status)
	}
	if calls < 4 {
		t.Fatalf("expected at least 4 polls through transitional states, got %d", calls)
	}
}

func TestPollUntilStatus_TimesOut(t *testing.T) {
	r := newTestVMResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"uuid": "vm-1", "status": "building"})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := r.pollUntilStatus(ctx, "vm-1", vmStatusRunning)
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
}

func TestPollUntilGone_WaitsForNotFound(t *testing.T) {
	var calls int32
	r := newTestVMResource(t, func(w http.ResponseWriter, req *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"uuid": "vm-1", "status": "deleting"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "not found"})
	})

	err := r.pollUntilGone(context.Background(), "vm-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls < 3 {
		t.Fatalf("expected at least 3 polls before disappearing, got %d", calls)
	}
}

func TestPollUntilGone_PropagatesUnexpectedErrors(t *testing.T) {
	r := newTestVMResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "boom"})
	})

	err := r.pollUntilGone(context.Background(), "vm-1")
	if err == nil {
		t.Fatal("expected a propagated error for a persistent 5xx, got nil")
	}
}
