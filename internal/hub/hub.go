package hub

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/williamokano/sentinelsnap/internal/observability"
)

const (
	EventSnapCreated  = "snap"
	EventSnapUpdated  = "snap_updated"
	EventSnapDeleted  = "snap_deleted"
	EventPhotoCreated = "photo"
	EventPhotoDeleted = "photo_deleted"
)

// heartbeatInterval controls how often ServeSSE writes a keepalive comment.
// Reverse proxies and load balancers reap idle connections, so the stream
// must carry periodic traffic even when no events are broadcast. It is a
// variable so tests can shorten it.
var heartbeatInterval = 30 * time.Second

type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
	metrics *observability.AppMetrics
}

func New(metrics *observability.AppMetrics) *Hub {
	return &Hub{
		clients: make(map[chan []byte]struct{}),
		metrics: metrics,
	}
}

func (h *Hub) Broadcast(ctx context.Context, eventType string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.ErrorContext(ctx, "hub: marshal error", "error", err)
		return
	}
	msg := append([]byte("event: "+eventType+"\ndata: "), data...)
	msg = append(msg, '\n', '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
	h.metrics.SSEBroadcast(ctx, eventType)
}

// ClientCount returns the number of currently connected SSE clients.
func (h *Hub) ClientCount() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return int64(len(h.clients))
}

// Close closes all active SSE client channels so that ServeSSE handlers
// return promptly and browsers see EOF, allowing them to reconnect.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		close(ch)
		delete(h.clients, ch)
	}
}

func (h *Hub) ServeSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan []byte, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
	}()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(msg); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
