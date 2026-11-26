package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRingBuffer(t *testing.T) {
	r := newRingBuffer(8)
	r.write([]byte("abcd"))
	if got, next := r.readSince(0, 0); string(got) != "abcd" || next != 4 {
		t.Fatalf("got (%q, %d), want (\"abcd\", 4)", got, next)
	}
	if got, next := r.readSince(2, 0); string(got) != "cd" || next != 4 {
		t.Fatalf("got (%q, %d), want (\"cd\", 4)", got, next)
	}

	// Wrap around buffer capacity (8): total=10, retained="cdefghij" (offsets 2..10)
	r.write([]byte("efghij"))
	if off := r.clampOffset(0); off != 2 {
		t.Fatalf("clampOffset(0) = %d, want 2", off)
	}
	if got, next := r.readSince(0, 3); string(got) != "cde" || next != 5 {
		t.Fatalf("chunk1 got (%q, %d), want (\"cde\", 5)", got, next)
	}
	if got, next := r.readSince(5, 10); string(got) != "fghij" || next != 10 {
		t.Fatalf("chunk2 got (%q, %d), want (\"fghij\", 10)", got, next)
	}

	// Single write larger than capacity
	r.write([]byte("0123456789ABCDEF"))
	if off := r.clampOffset(0); off != 18 {
		t.Fatalf("clampOffset after large write = %d, want 18", off)
	}
	if got, next := r.readSince(0, 0); string(got) != "89ABCDEF" || next != 26 {
		t.Fatalf("large write got (%q, %d), want (\"89ABCDEF\", 26)", got, next)
	}
}

type readyMsg struct {
	T          string `json:"t"`
	PID        int    `json:"pid"`
	Session    string `json:"session"`
	Reattached bool   `json:"reattached"`
	Offset     uint64 `json:"offset"`
	Code       string `json:"code"`
}

func startTestWSServer(t *testing.T, target Target) (*httptest.Server, func(query url.Values) *websocket.Conn) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ws.SetReadLimit(4 * 1024 * 1024)
		c := newWSClient(ws)
		q := r.URL.Query()
		cols := clampSize(atoi(q.Get("cols")), 80, 1000)
		rows := clampSize(atoi(q.Get("rows")), 24, 500)
		if sid := q.Get("session"); sid != "" {
			s := sessions.get(sid)
			if s == nil {
				_ = c.sendJSON(map[string]string{"t": "error", "code": "session_expired"})
				c.closeNormal()
				return
			}
			since, _ := strconv.ParseUint(q.Get("since"), 10, 64)
			s.attach(c, since, cols, rows, true)
			return
		}
		s, err := startPTYSession(target, cols, rows)
		if err != nil {
			_ = c.sendJSON(map[string]string{"t": "error", "code": "spawn_failed", "detail": err.Error()})
			c.closeNormal()
			return
		}
		s.attach(c, 0, cols, rows, false)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		// Ensure all active sessions & HTTP handlers exit before restoring package timers
		sessions.mu.Lock()
		all := make([]*ptySession, 0, len(sessions.sessions))
		for _, s := range sessions.sessions {
			all = append(all, s)
		}
		sessions.mu.Unlock()
		for _, s := range all {
			s.terminate()
		}
		srv.Close()
	})

	dial := func(query url.Values) *websocket.Conn {
		t.Helper()
		u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
		if len(query) > 0 {
			u += "?" + query.Encode()
		}
		h := http.Header{"Origin": {srv.URL}}
		ws, _, err := websocket.DefaultDialer.Dial(u, h)
		if err != nil {
			t.Fatalf("dial %s: %v", u, err)
		}
		return ws
	}
	return srv, dial
}

func readReady(t *testing.T, ws *websocket.Conn) readyMsg {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	mt, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read ready: %v", err)
	}
	if mt != websocket.TextMessage {
		t.Fatalf("expected text ready frame, got mt=%d", mt)
	}
	var m readyMsg
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal ready: %v", err)
	}
	return m
}

func readUntilContains(t *testing.T, ws *websocket.Conn, substr string, rxBytes *uint64) string {
	t.Helper()
	var sb strings.Builder
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = ws.SetReadDeadline(deadline)
		mt, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("read while waiting for %q (got %q): %v", substr, sb.String(), err)
		}
		if mt == websocket.BinaryMessage {
			if rxBytes != nil {
				*rxBytes += uint64(len(data))
			}
			sb.Write(data)
			if strings.Contains(sb.String(), substr) {
				return sb.String()
			}
		}
	}
	t.Fatalf("timed out waiting for %q; got %q", substr, sb.String())
	return ""
}

func TestSessionDetachAndReattachPreservesProcessAndReplay(t *testing.T) {
	origGrace := detachGracePeriod
	detachGracePeriod = 5 * time.Second
	t.Cleanup(func() { detachGracePeriod = origGrace })

	target := Target{ID: "test-sh", Kind: "command", Command: []string{"/bin/sh"}}
	_, dial := startTestWSServer(t, target)

	ws1 := dial(url.Values{"target": {"test-sh"}, "cols": {"80"}, "rows": {"24"}})
	ready1 := readReady(t, ws1)
	if ready1.T != "ready" || ready1.Session == "" || ready1.Reattached {
		t.Fatalf("unexpected initial ready: %+v", ready1)
	}

	var rxBytes uint64 = ready1.Offset
	// Wait for initial shell prompt so input is handled by sh rather than early PTY echo
	readUntilContains(t, ws1, "$ ", &rxBytes)

	// Use printf with split tokens so the echoed command line does not match the evaluated output
	cmd := "MYVAR=still_here_9876; printf 'BEFORE_%s\\n' 'DETACH'; (sleep 0.25; printf 'AFTER_%s\\n' 'DETACH') &\n"
	if err := ws1.WriteJSON(map[string]string{"t": "in", "d": cmd}); err != nil {
		t.Fatalf("write input: %v", err)
	}
	readUntilContains(t, ws1, "BEFORE_DETACH", &rxBytes)

	// Simulate browser freezing/killing the WebSocket without sending {"t":"close"}
	_ = ws1.Close()

	// Wait for the backgrounded command to print while detached
	time.Sleep(450 * time.Millisecond)

	// Reattach using saved session ID and byte offset
	ws2 := dial(url.Values{
		"target":  {"test-sh"},
		"session": {ready1.Session},
		"since":   {strconv.FormatUint(rxBytes, 10)},
		"cols":    {"80"},
		"rows":    {"24"},
	})
	defer ws2.Close()

	ready2 := readReady(t, ws2)
	if ready2.T != "ready" || !ready2.Reattached || ready2.PID != ready1.PID || ready2.Session != ready1.Session {
		t.Fatalf("unexpected reattach ready: %+v (want pid=%d session=%s)", ready2, ready1.PID, ready1.Session)
	}
	rxBytes = ready2.Offset

	// Missed output from detached window must be replayed without repeating earlier BEFORE_DETACH
	replayed := readUntilContains(t, ws2, "AFTER_DETACH", &rxBytes)
	if strings.Contains(replayed, "BEFORE_DETACH") {
		t.Fatalf("expected no duplicate earlier output, got %q", replayed)
	}

	// Verify the shell variable from before disconnect is still intact
	if err := ws2.WriteJSON(map[string]string{"t": "in", "d": "printf 'VAR=%s\\n' \"$MYVAR\"\n"}); err != nil {
		t.Fatalf("write verify input: %v", err)
	}
	readUntilContains(t, ws2, "VAR=still_here_9876", &rxBytes)

	// Explicit close must terminate immediately
	if err := ws2.WriteJSON(map[string]string{"t": "close"}); err != nil {
		t.Fatalf("write close: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sessions.get(ready1.Session) == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session %s still present after explicit close", ready1.Session)
}

func TestSessionDetachGraceTimeout(t *testing.T) {
	origGrace := detachGracePeriod
	detachGracePeriod = 200 * time.Millisecond
	t.Cleanup(func() { detachGracePeriod = origGrace })

	target := Target{ID: "test-timeout", Kind: "command", Command: []string{"/bin/sh"}}
	_, dial := startTestWSServer(t, target)

	ws1 := dial(url.Values{"target": {"test-timeout"}})
	ready := readReady(t, ws1)
	if ready.T != "ready" || ready.Session == "" {
		t.Fatalf("unexpected ready: %+v", ready)
	}
	_ = ws1.Close()

	// Wait past grace period
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && sessions.get(ready.Session) != nil {
		time.Sleep(25 * time.Millisecond)
	}
	if sessions.get(ready.Session) != nil {
		t.Fatalf("session did not expire after detach grace period")
	}

	// Reattaching after expiration must return session_expired error
	ws2 := dial(url.Values{"target": {"test-timeout"}, "session": {ready.Session}})
	defer ws2.Close()
	msg := readReady(t, ws2)
	if msg.T != "error" || msg.Code != "session_expired" {
		t.Fatalf("got %+v, want error/session_expired", msg)
	}
}

func TestSessionPingPongKeepalive(t *testing.T) {
	origPing, origPong, origGrace := wsPingInterval, wsPongWait, detachGracePeriod
	wsPingInterval = 50 * time.Millisecond
	wsPongWait = 500 * time.Millisecond
	detachGracePeriod = 2 * time.Second
	t.Cleanup(func() {
		wsPingInterval, wsPongWait, detachGracePeriod = origPing, origPong, origGrace
	})

	target := Target{ID: "test-ping", Kind: "command", Command: []string{"/bin/sh"}}
	_, dial := startTestWSServer(t, target)

	ws := dial(url.Values{"target": {"test-ping"}})
	defer func() {
		_ = ws.WriteJSON(map[string]string{"t": "close"})
		_ = ws.Close()
	}()

	var pings atomic.Int32
	ws.SetPingHandler(func(appData string) error {
		pings.Add(1)
		return ws.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
	})

	ready := readReady(t, ws)
	if ready.T != "ready" {
		t.Fatalf("unexpected ready: %+v", ready)
	}

	// Test client-initiated app-level ping -> pong
	if err := ws.WriteJSON(map[string]string{"t": "ping"}); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	gotPong := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (!gotPong || pings.Load() < 2) {
		_ = ws.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		mt, data, err := ws.ReadMessage()
		if err != nil {
			continue
		}
		if mt == websocket.TextMessage {
			var m struct {
				T string `json:"t"`
			}
			if json.Unmarshal(data, &m) == nil && m.T == "pong" {
				gotPong = true
			}
		}
	}
	if !gotPong {
		t.Fatalf("did not receive app-level pong")
	}
	if pings.Load() < 2 {
		t.Fatalf("expected >=2 server RFC 6455 pings, got %d", pings.Load())
	}
}
