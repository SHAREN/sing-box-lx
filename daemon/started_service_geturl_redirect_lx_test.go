//go:build with_lx_command

package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// A caller's arbitrary headers are credentials until proven otherwise. The
// standard Go redirect copier forwards X-Goog-Api-Key to unrelated origins.
func TestGetURLViaOutbound_CredentialHeaderCannotCrossOrigin_LX(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/landing", http.StatusFound)
	}))
	defer source.Close()

	response, err := newProbeService(&probeDialer{}).GetURLViaOutbound(context.Background(), &GetURLViaOutboundRequest{
		OutboundTag: "node",
		Link:        source.URL,
		Headers: []*HttpHeaderPair{
			{Key: "x-goog-api-key", Value: "TEST_ONLY_DO_NOT_USE"},
		},
	})
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	if response.Error != "" {
		t.Fatalf("payload error: %s", response.Error)
	}
	if response.HttpStatus != http.StatusFound {
		t.Fatalf("status = %d, want 302 from the original host", response.HttpStatus)
	}
	if targetRequests.Load() != 0 {
		t.Fatal("redirect must not send any request to a different authority")
	}

	// Without user-supplied headers, retain the established diagnostics API:
	// redirects are followed and a final response is returned.
	plain, err := newProbeService(&probeDialer{}).GetURLViaOutbound(context.Background(), &GetURLViaOutboundRequest{
		OutboundTag: "node",
		Link:        source.URL,
	})
	if err != nil || plain.Error != "" || plain.HttpStatus != http.StatusOK {
		t.Fatalf("headerless redirect must remain functional: response=%+v err=%v", plain, err)
	}
	if targetRequests.Load() != 1 {
		t.Fatalf("headerless redirect should reach target once, got %d", targetRequests.Load())
	}
}

func TestGetURLViaOutbound_CredentialHeaderSameOriginRedirect_LX(t *testing.T) {
	var source *httptest.Server
	source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/landing" {
			if r.Header.Get("X-Goog-Api-Key") != "TEST_ONLY_DO_NOT_USE" {
				t.Error("same-origin redirect lost its authorized header")
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/landing", http.StatusMovedPermanently)
	}))
	defer source.Close()

	response, err := newProbeService(&probeDialer{}).GetURLViaOutbound(context.Background(), &GetURLViaOutboundRequest{
		OutboundTag: "node",
		Link:        source.URL,
		Headers: []*HttpHeaderPair{
			{Key: "x-goog-api-key", Value: "TEST_ONLY_DO_NOT_USE"},
		},
	})
	if err != nil || response.Error != "" || response.HttpStatus != http.StatusOK {
		t.Fatalf("same-origin redirect must work: response=%+v err=%v", response, err)
	}
}

func TestGetURLViaOutbound_CredentialHeaderSchemeDowngrade_LX(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://"+r.Host+"/landing", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	response, err := newProbeService(&probeDialer{}).GetURLViaOutbound(context.Background(), &GetURLViaOutboundRequest{
		OutboundTag: "node",
		Link:        source.URL,
		Headers:     []*HttpHeaderPair{{Key: "x-goog-api-key", Value: "TEST_ONLY_DO_NOT_USE"}},
	})
	if err != nil || response.Error != "" || response.HttpStatus != http.StatusTemporaryRedirect {
		t.Fatalf("redirect to different scheme must be rejected: response=%+v err=%v", response, err)
	}
}
