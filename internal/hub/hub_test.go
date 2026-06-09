package hub_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/williamokano/sentinelsnap/internal/hub"
)

func TestBroadcast_ZeroClients(t *testing.T) {
	h := hub.New(nil)
	// Must not panic or block when there are no connected clients.
	assert.NotPanics(t, func() {
		h.Broadcast(context.Background(), hub.EventSnapCreated, map[string]int{"id": 1})
	})
}

func TestBroadcast_SlowClient_NonBlockingSkip(t *testing.T) {
	h := hub.New(nil)

	// Connect a real SSE client over an in-process test server so the hub's
	// select/default path can be exercised without blocking the test goroutine.
	//
	// The client reads headers but never drains the body, so the hub's
	// channel buffer fills up after 8 messages and subsequent Broadcasts
	// must be dropped (non-blocking) rather than blocking.

	// Use a pipe so we can control exactly when the client "receives" bytes.
	sseReady := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(sseReady) // signal that ServeSSE is about to be called
		h.ServeSSE(w, r)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the SSE request in a goroutine; we need a goroutine that reads
	// headers so the HTTP handshake completes, but doesn't drain the body.
	connectedCh := make(chan struct{})
	go func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
		// We need to trigger the first flush so the server actually sends headers.
		// Send an event before connecting so the select is unblocked and headers go out.
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			close(connectedCh)
			// Deliberately do not read resp.Body so the send buffer fills up.
			defer func() { _ = resp.Body.Close() }()
			<-ctx.Done()
		}
	}()

	// Trigger a broadcast to unblock ServeSSE's select so it sends headers.
	<-sseReady
	time.Sleep(10 * time.Millisecond)
	h.Broadcast(context.Background(), hub.EventSnapCreated, map[string]int{"id": 0})

	select {
	case <-connectedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE client did not connect in time")
	}

	// Now flood the hub far beyond its per-client channel buffer (8).
	// If Broadcast blocks on any send, this will time out.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 1; i <= 50; i++ {
			h.Broadcast(context.Background(), hub.EventSnapCreated, map[string]int{"id": i})
		}
	}()

	select {
	case <-done:
		// All broadcasts completed without blocking.
	case <-time.After(3 * time.Second):
		t.Fatal("Broadcast blocked on a slow client")
	}
}

func TestServeSSE_Heartbeat_PingsIdleConnection(t *testing.T) {
	restore := hub.SetHeartbeatInterval(50 * time.Millisecond)
	defer restore()

	h := hub.New(nil)

	server := httptest.NewServer(http.HandlerFunc(h.ServeSSE))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}

	// No broadcasts are sent: the heartbeat alone must flush headers and
	// produce traffic on an otherwise idle connection.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	scanner := bufio.NewScanner(resp.Body)
	received := make(chan string, 1)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, ": ping") {
				received <- line
				return
			}
		}
	}()

	select {
	case line := <-received:
		assert.Equal(t, ": ping", line)
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive heartbeat ping before timeout")
	}
}

func TestServeSSE_ClientDisconnect_TriggersCleanup(t *testing.T) {
	h := hub.New(nil)

	server := httptest.NewServer(http.HandlerFunc(h.ServeSSE))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Start the client; ServeSSE will block in select, waiting for a message.
	// We need to unblock it with an event first so headers are flushed and Do() returns.
	go func() {
		// Trigger a broadcast so the server writes something and flushes headers.
		time.Sleep(20 * time.Millisecond)
		h.Broadcast(context.Background(), hub.EventSnapCreated, map[string]int{"id": 1})
	}()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	// Confirm the event arrives at the client.
	scanner := bufio.NewScanner(resp.Body)
	received := make(chan string, 1)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event:") {
				received <- line
				return
			}
		}
	}()

	select {
	case line := <-received:
		assert.Contains(t, line, hub.EventSnapCreated)
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive SSE event before timeout")
	}

	// Disconnect the client.
	cancel()
	_ = resp.Body.Close()

	// After disconnect, ServeSSE's defer must remove the client from the hub.
	// Give the server goroutine time to process the cancellation.
	time.Sleep(50 * time.Millisecond)

	// A subsequent broadcast must not panic or block.
	assert.NotPanics(t, func() {
		h.Broadcast(context.Background(), hub.EventSnapDeleted, map[string]int{"id": 1})
	})
}
