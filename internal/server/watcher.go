package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/pastelocal/pastelocal/internal/auth"
	cliperr "github.com/pastelocal/pastelocal/internal/errors"
	"github.com/pastelocal/pastelocal/internal/proto"
)

// WatchHub manages WebSocket connections for clipboard change notifications.
type WatchHub struct {
	mu          sync.Mutex
	subscribers map[*WatchSubscriber]struct{}
	logger      *slog.Logger
}

// WatchSubscriber represents a single WebSocket connection.
type WatchSubscriber struct {
	hub   *WatchHub
	token string
	conn  *websocket.Conn
	send  chan []byte
}

// NewWatchHub creates a new WatchHub.
func NewWatchHub(logger *slog.Logger) *WatchHub {
	return &WatchHub{
		subscribers: make(map[*WatchSubscriber]struct{}),
		logger:      logger,
	}
}

// Subscribe adds a new WebSocket subscriber.
func (h *WatchHub) Subscribe(conn *websocket.Conn, token string) *WatchSubscriber {
	sub := &WatchSubscriber{
		hub:   h,
		token: token,
		conn:  conn,
		send:  make(chan []byte, 16),
	}
	h.mu.Lock()
	h.subscribers[sub] = struct{}{}
	h.mu.Unlock()
	h.logger.Info("watch subscriber added", "active", len(h.subscribers))
	return sub
}

// Unsubscribe removes a WebSocket subscriber.
func (h *WatchHub) Unsubscribe(sub *WatchSubscriber) {
	h.mu.Lock()
	delete(h.subscribers, sub)
	h.mu.Unlock()
	close(sub.send)
	h.logger.Info("watch subscriber removed", "active", len(h.subscribers))
}

// Notify sends a clipboard change notification to all subscribers.
func (h *WatchHub) Notify(notification proto.WatchNotification) {
	data, err := json.Marshal(notification)
	if err != nil {
		h.logger.Error("failed to marshal watch notification", "err", err)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for sub := range h.subscribers {
		select {
		case sub.send <- data:
		default:
			h.logger.Warn("dropping notification for slow subscriber")
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (h *WatchHub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subscribers)
}

// ReadPump reads messages from the WebSocket connection to detect close frames.
func (s *WatchSubscriber) ReadPump() {
	defer func() {
		s.hub.Unsubscribe(s)
		s.conn.CloseNow()
	}()

	for {
		_, _, err := s.conn.Read(context.Background())
		if err != nil {
			break
		}
	}
}

// WritePump writes notifications to the WebSocket connection.
func (s *WatchSubscriber) WritePump() {
	defer s.conn.CloseNow()

	for msg := range s.send {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := s.conn.Write(ctx, websocket.MessageText, msg)
		cancel()
		if err != nil {
			break
		}
	}
}

// handleWatch handles the WS /clipboard/watch endpoint.
func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	// Validate auth token before upgrading to WebSocket.
	_, authErr := validateAuthFromWS(r, s.tokenStore)
	if authErr != nil {
		cliperr.WriteJSON(w, authErr)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"127.0.0.1", "localhost"},
	})
	if err != nil {
		s.logger.Error("websocket accept failed", "err", err)
		return
	}

	token, _ := validateAuthFromWS(r, s.tokenStore)
	sub := s.watchHub.Subscribe(conn, token)
	go sub.ReadPump()
	go sub.WritePump()
}

// validateAuthFromWS extracts and validates the token for WebSocket connections.
func validateAuthFromWS(r *http.Request, tokenStore *auth.TokenStore) (string, *cliperr.Error) {
	// Try query parameter first (WebSocket clients may not be able to set headers).
	tokenParam := r.URL.Query().Get("token")
	if tokenParam != "" {
		expected, err := tokenStore.Retrieve()
		if err != nil {
			return "", cliperr.New("CB2001")
		}
		if auth.ValidateToken(tokenParam, expected) {
			return tokenParam, nil
		}
		return "", cliperr.New("CB2001")
	}

	// Fall back to Authorization header.
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", cliperr.New("CB2002")
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", cliperr.New("CB2001")
	}
	provided := parts[1]
	expected, err := tokenStore.Retrieve()
	if err != nil {
		return "", cliperr.New("CB2001")
	}
	if !auth.ValidateToken(provided, expected) {
		return "", cliperr.New("CB2001")
	}
	return provided, nil
}
