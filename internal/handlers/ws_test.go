package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"

	"github.com/iag-finance/backend/internal/authclient"
)

// A connection with no Bearer/?token and whose first frame is not a valid auth
// frame must be rejected with auth.error (the verifier is never reached because
// no token is resolved).
func TestWSRejectsMissingAuthFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Non-nil verifier so we exercise the token-resolution path, not the
	// defensive nil-verifier short-circuit. Verify is not called when no token
	// is present, so the dummy JWKS URL is never fetched.
	h := &WSHandler{Verifier: authclient.NewVerifier("http://127.0.0.1:1/jwks", "iss", "aud")}
	r.GET("/v1/ws/events", h.Events)
	srv := httptest.NewServer(r)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/ws/events"
	conn, err := websocket.Dial(wsURL, "", srv.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := websocket.JSON.Send(conn, map[string]any{"type": "hello"}); err != nil {
		t.Fatalf("send: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var msg map[string]any
	if err := websocket.JSON.Receive(conn, &msg); err != nil {
		t.Fatalf("receive: %v", err)
	}
	if msg["type"] != "auth.error" {
		t.Fatalf("expected auth.error, got %v", msg["type"])
	}
}
