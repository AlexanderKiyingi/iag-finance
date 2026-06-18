package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"

	"github.com/iag-finance/backend/internal/authclient"
	"github.com/iag-finance/backend/internal/repository"
)

// WSHandler serves GET /v1/ws/events, the realtime channel the finance SPA
// expects. The browser cannot set an Authorization header on a WebSocket, so
// auth is done over the socket: the client sends {"type":"auth","token":...}
// as its first frame and we reply {"type":"auth.ok"} or {"type":"auth.error"}.
//
// We then keep the socket alive with periodic {"type":"ping"} frames (the client
// replies "pong") and push {"type":"audit.event"} whenever the audit tail
// advances, which tells the SPA to refresh. The frame carries no payload — the
// client re-fetches through the normal authorized REST endpoints — so no data
// is exposed beyond "something changed".
type WSHandler struct {
	Verifier *authclient.Verifier
	Repo     *repository.Repository

	// Tunables (zero values fall back to sensible defaults).
	PingInterval time.Duration
	TailInterval time.Duration
	AuthTimeout  time.Duration
}

type wsInbound struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

// bearerOrQueryToken pulls the access token from the upgrade request: the
// Authorization: Bearer header the gateway injects (from ?token=), or a direct
// ?token= query for connections that bypass the gateway. Returns "" if neither.
func bearerOrQueryToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	return r.URL.Query().Get("token")
}

func (h *WSHandler) pingInterval() time.Duration {
	if h.PingInterval > 0 {
		return h.PingInterval
	}
	return 25 * time.Second
}

func (h *WSHandler) tailInterval() time.Duration {
	if h.TailInterval > 0 {
		return h.TailInterval
	}
	return 10 * time.Second
}

func (h *WSHandler) authTimeout() time.Duration {
	if h.AuthTimeout > 0 {
		return h.AuthTimeout
	}
	return 10 * time.Second
}

// Events upgrades the connection and runs the realtime loop. Auth is permissive
// at the handshake (origin is not checked) because the token frame authenticates
// the session; this also keeps it working behind the API gateway proxy.
func (h *WSHandler) Events(c *gin.Context) {
	ctx := c.Request.Context()
	srv := websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler:   func(ws *websocket.Conn) { h.serve(ctx, ws) },
	}
	srv.ServeHTTP(c.Writer, c.Request)
}

func (h *WSHandler) serve(ctx context.Context, ws *websocket.Conn) {
	defer ws.Close()

	if h.Verifier == nil {
		_ = websocket.JSON.Send(ws, gin.H{"type": "auth.error", "message": "auth unavailable"})
		return
	}

	// 1. Resolve the access token. Through the gateway it arrives as an
	//    Authorization: Bearer header (lifted from ?token= by the gateway, since
	//    browsers can't set headers on a WS); a direct ?token= is also accepted.
	//    Failing both, fall back to the in-band {type:"auth",token} frame — with
	//    a deadline so an idle/garbage connection can't hold the socket open.
	token := bearerOrQueryToken(ws.Request())
	if token == "" {
		_ = ws.SetReadDeadline(time.Now().Add(h.authTimeout()))
		var in wsInbound
		if err := websocket.JSON.Receive(ws, &in); err == nil && in.Type == "auth" {
			token = in.Token
		}
		_ = ws.SetReadDeadline(time.Time{}) // clear: the socket is now long-lived
	}
	if token == "" {
		_ = websocket.JSON.Send(ws, gin.H{"type": "auth.error", "message": "auth required"})
		return
	}
	if _, _, err := h.Verifier.Verify(token); err != nil {
		_ = websocket.JSON.Send(ws, gin.H{"type": "auth.error", "message": "invalid token"})
		return
	}
	if err := websocket.JSON.Send(ws, gin.H{"type": "auth.ok"}); err != nil {
		return
	}

	// 2. Reader goroutine drains client frames (pong, etc.) and signals close.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var msg map[string]any
			if err := websocket.JSON.Receive(ws, &msg); err != nil {
				return
			}
		}
	}()

	// 3. Writer loop: keepalive pings + push on audit-tail change. All writes
	//    happen here (single goroutine) so the conn is never written concurrently.
	ping := time.NewTicker(h.pingInterval())
	tail := time.NewTicker(h.tailInterval())
	defer ping.Stop()
	defer tail.Stop()

	last := h.auditFingerprint(ctx)

	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ping.C:
			if err := websocket.JSON.Send(ws, gin.H{"type": "ping"}); err != nil {
				return
			}
		case <-tail.C:
			fp := h.auditFingerprint(ctx)
			if fp != "" && fp != last {
				last = fp
				if err := websocket.JSON.Send(ws, gin.H{"type": "audit.event"}); err != nil {
					return
				}
			}
		}
	}
}

// auditFingerprint returns a cheap identity of the latest audit entry, or "" if
// none / unavailable. A change between polls means the ledger advanced.
func (h *WSHandler) auditFingerprint(ctx context.Context) string {
	if h.Repo == nil {
		return ""
	}
	entries, _, err := h.Repo.ListAuditLogs(ctx, repository.AuditListFilter{Limit: 1})
	if err != nil || len(entries) == 0 {
		return ""
	}
	e := entries[0]
	return e.ID.String() + ":" + e.CreatedAt.Format(time.RFC3339Nano)
}
