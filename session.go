package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var (
	wsPingInterval     = 25 * time.Second
	wsPongWait         = 75 * time.Second
	wsWriteWait        = 15 * time.Second
	detachGracePeriod  = 10 * time.Minute
	sessionReplayBytes = 512 * 1024
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
	CheckOrigin:     originAllowed,
}

// 浏览器 → 服务端：二进制帧或 {"t":"in","d":...} 是输入，{"t":"resize","cols","rows"} 调整尺寸，
// {"t":"ping"} 探测连接活性，{"t":"close"} 主动结束会话
// 服务端 → 浏览器：二进制帧是终端输出，文本帧是 {"t":"ready"|"exit"|"error"|"pong"}
type controlMsg struct {
	T    string `json:"t"`
	D    string `json:"d"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// 有界环形字节缓冲，同时维护自会话启动以来的单调字节总数，用于断线重连后无重复补发输出
type ringBuffer struct {
	buf   []byte
	head  int
	size  int
	total uint64
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{buf: make([]byte, capacity)}
}

func (r *ringBuffer) write(p []byte) {
	if len(p) == 0 {
		return
	}
	capN := len(r.buf)
	if len(p) >= capN {
		copy(r.buf, p[len(p)-capN:])
		r.head = 0
		r.size = capN
		r.total += uint64(len(p))
		return
	}
	tail := (r.head + r.size) % capN
	first := min(len(p), capN-tail)
	copy(r.buf[tail:], p[:first])
	if rest := len(p) - first; rest > 0 {
		copy(r.buf[0:], p[first:])
	}
	if r.size+len(p) > capN {
		overflow := r.size + len(p) - capN
		r.head = (r.head + overflow) % capN
		r.size = capN
	} else {
		r.size += len(p)
	}
	r.total += uint64(len(p))
}

func (r *ringBuffer) clampOffset(offset uint64) uint64 {
	base := r.total - uint64(r.size)
	if offset < base {
		return base
	}
	if offset > r.total {
		return r.total
	}
	return offset
}

func (r *ringBuffer) readSince(offset uint64, maxBytes int) (out []byte, next uint64) {
	offset = r.clampOffset(offset)
	if offset >= r.total {
		return nil, r.total
	}
	avail := int(r.total - offset)
	if maxBytes > 0 && avail > maxBytes {
		avail = maxBytes
	}
	base := r.total - uint64(r.size)
	skip := int(offset - base)
	start := (r.head + skip) % len(r.buf)
	out = make([]byte, avail)
	first := min(avail, len(r.buf)-start)
	copy(out, r.buf[start:start+first])
	if avail > first {
		copy(out[first:], r.buf[:avail-first])
	}
	return out, offset + uint64(avail)
}

type wsClient struct {
	ws      *websocket.Conn
	writeMu sync.Mutex
	notify  chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func newWSClient(ws *websocket.Conn) *wsClient {
	return &wsClient{
		ws:     ws,
		notify: make(chan struct{}, 1),
		closed: make(chan struct{}),
	}
}

func (c *wsClient) signal() {
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

func (c *wsClient) sendJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
	return c.ws.WriteJSON(v)
}

func (c *wsClient) sendBinary(b []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
	return c.ws.WriteMessage(websocket.BinaryMessage, b)
}

func (c *wsClient) sendPing() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(wsWriteWait))
}

func (c *wsClient) closeNormal() {
	c.once.Do(func() {
		close(c.closed)
		c.writeMu.Lock()
		_ = c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		_ = c.ws.Close()
		c.writeMu.Unlock()
	})
}

func (c *wsClient) closeAbort() {
	c.once.Do(func() {
		close(c.closed)
		_ = c.ws.Close()
	})
}

type ptySession struct {
	id       string
	targetID string
	cmd      *exec.Cmd
	ptmx     *os.File
	pid      int

	mu          sync.Mutex
	client      *wsClient
	replay      *ringBuffer
	cols        uint16
	rows        uint16
	detachTimer *time.Timer
	exited      bool
	exitCode    int
	exitSignal  int
	closing     bool

	outputDone chan struct{}
	exitDone   chan struct{}
}

type sessionManager struct {
	mu       sync.Mutex
	sessions map[string]*ptySession
}

var sessions = &sessionManager{sessions: make(map[string]*ptySession)}

func (m *sessionManager) put(s *ptySession) {
	m.mu.Lock()
	m.sessions[s.id] = s
	m.mu.Unlock()
}

func (m *sessionManager) get(id string) *ptySession {
	m.mu.Lock()
	s := m.sessions[id]
	m.mu.Unlock()
	return s
}

func (m *sessionManager) del(id string, s *ptySession) {
	m.mu.Lock()
	if m.sessions[id] == s {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
}

func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func clampSize(v, def, maxVal int) uint16 {
	if v > 0 && v <= maxVal {
		return uint16(v)
	}
	return uint16(def)
}

func startPTYSession(target Target, cols, rows uint16) (*ptySession, error) {
	spec := spawnSpec(target)
	cmd := exec.Command(spec.File, spec.Args...)
	cmd.Dir = spec.Cwd
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=infiterm")
	if os.Getenv("LANG") == "" {
		cmd.Env = append(cmd.Env, "LANG=en_US.UTF-8")
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}

	s := &ptySession{
		id:         newSessionID(),
		targetID:   target.ID,
		cmd:        cmd,
		ptmx:       ptmx,
		pid:        cmd.Process.Pid,
		replay:     newRingBuffer(sessionReplayBytes),
		cols:       cols,
		rows:       rows,
		outputDone: make(chan struct{}),
		exitDone:   make(chan struct{}),
	}
	sessions.put(s)
	log.Printf("[session] open %s id=%s pid=%d", target.ID, shortID(s.id), s.pid)

	// pty → 环形缓冲，并唤醒当前附着的客户端发送。
	// 即使客户端断开也要持续读取：一方面保留断线期间的输出供重连回放，
	// 另一方面 macOS 上进程退出时要等 tty 输出排空，没人读的话会卡在退出状态，连 SIGKILL 都收不了尾
	go func() {
		defer close(s.outputDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.replay.write(buf[:n])
				c := s.client
				s.mu.Unlock()
				if c != nil {
					c.signal()
				}
			}
			if err != nil {
				return
			}
		}
	}()

	go func() {
		defer close(s.exitDone)
		defer ptmx.Close()
		cmd.Wait()
		// 把进程退出前的最后一段输出读完；后台子进程仍占着 pty 时不无限等待
		select {
		case <-s.outputDone:
		case <-time.After(300 * time.Millisecond):
		}
		code, signal := exitInfo(cmd.ProcessState)
		log.Printf("[session] exit %s id=%s pid=%d code=%d", target.ID, shortID(s.id), s.pid, code)

		sessions.del(s.id, s)
		s.mu.Lock()
		s.exited = true
		s.exitCode = code
		s.exitSignal = signal
		if s.detachTimer != nil {
			s.detachTimer.Stop()
			s.detachTimer = nil
		}
		c := s.client
		s.mu.Unlock()
		if c != nil {
			c.signal()
		}
	}()

	return s, nil
}

func (s *ptySession) resize(cols, rows uint16) {
	s.mu.Lock()
	if s.exited || (s.cols == cols && s.rows == rows) {
		s.mu.Unlock()
		return
	}
	s.cols, s.rows = cols, rows
	s.mu.Unlock()
	_ = pty.Setsize(s.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

// 主动关闭或宽限期超时：先 SIGHUP 整个进程组（和关闭终端窗口一样），3 秒内没退出再强杀
func (s *ptySession) terminate() {
	sessions.del(s.id, s)
	s.mu.Lock()
	if s.exited || s.closing {
		s.mu.Unlock()
		return
	}
	s.closing = true
	if s.detachTimer != nil {
		s.detachTimer.Stop()
		s.detachTimer = nil
	}
	c := s.client
	s.client = nil
	s.mu.Unlock()

	if c != nil {
		c.closeNormal()
	}
	_ = syscall.Kill(-s.pid, syscall.SIGHUP)
	select {
	case <-s.exitDone:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-s.pid, syscall.SIGKILL)
		<-s.exitDone
	}
}

func (s *ptySession) detach(c *wsClient, readErr error) {
	s.mu.Lock()
	if s.client != c || s.exited || s.closing {
		s.mu.Unlock()
		return
	}
	s.client = nil
	if s.detachTimer != nil {
		s.detachTimer.Stop()
	}
	s.detachTimer = time.AfterFunc(detachGracePeriod, func() {
		s.mu.Lock()
		stillDetached := s.client == nil && !s.exited && !s.closing
		s.mu.Unlock()
		if stillDetached {
			log.Printf("[session] timeout %s id=%s pid=%d", s.targetID, shortID(s.id), s.pid)
			s.terminate()
		}
	})
	s.mu.Unlock()
	log.Printf("[session] detach %s id=%s pid=%d err=%v", s.targetID, shortID(s.id), s.pid, readErr)
}

func (s *ptySession) attach(c *wsClient, since uint64, cols, rows uint16, reattached bool) {
	s.mu.Lock()
	if s.exited || s.closing {
		s.mu.Unlock()
		_ = c.sendJSON(map[string]string{"t": "error", "code": "session_expired"})
		c.closeNormal()
		return
	}
	if s.detachTimer != nil {
		s.detachTimer.Stop()
		s.detachTimer = nil
	}
	prev := s.client
	s.client = c
	startOffset := s.replay.clampOffset(since)
	s.mu.Unlock()

	if prev != nil {
		prev.closeAbort()
	}
	if reattached {
		log.Printf("[session] reattach %s id=%s pid=%d offset=%d", s.targetID, shortID(s.id), s.pid, startOffset)
		s.resize(cols, rows)
	}

	if err := c.sendJSON(map[string]any{
		"t":          "ready",
		"pid":        s.pid,
		"session":    s.id,
		"reattached": reattached,
		"offset":     startOffset,
	}); err != nil {
		c.closeAbort()
		s.detach(c, err)
		return
	}

	// 服务端定时发 RFC 6455 Ping，浏览器网络层自动回 Pong，不受后台标签页 JS 定时器节流影响
	pingInterval := wsPingInterval
	pongWait := wsPongWait
	_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		return c.ws.SetReadDeadline(time.Now().Add(pongWait))
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.sendPing(); err != nil {
					c.closeAbort()
					return
				}
			case <-c.closed:
				return
			}
		}
	}()

	// 按游标顺序发送：先补齐断连期间的环形缓冲，再无缝流式推送后续输出
	go func() {
		defer wg.Done()
		cursor := startOffset
		for {
			s.mu.Lock()
			if s.client != c {
				s.mu.Unlock()
				return
			}
			chunk, next := s.replay.readSince(cursor, 32*1024)
			exited := s.exited
			code, sig := s.exitCode, s.exitSignal
			s.mu.Unlock()

			if len(chunk) > 0 {
				if err := c.sendBinary(chunk); err != nil {
					c.closeAbort()
					return
				}
				cursor = next
				continue
			}
			if exited {
				_ = c.sendJSON(map[string]any{"t": "exit", "code": code, "signal": sig})
				c.closeNormal()
				return
			}
			select {
			case <-c.notify:
			case <-c.closed:
				return
			}
		}
	}()

	// 浏览器 → pty，直到连接断开或主动关闭
	var readErr error
	explicitClose := false
	for {
		mt, data, err := c.ws.ReadMessage()
		if err != nil {
			readErr = err
			break
		}
		_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
		if mt == websocket.BinaryMessage {
			_, _ = s.ptmx.Write(data)
			continue
		}
		var msg controlMsg
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.T {
		case "in":
			_, _ = s.ptmx.Write([]byte(msg.D))
		case "resize":
			s.resize(clampSize(msg.Cols, 80, 1000), clampSize(msg.Rows, 24, 500))
		case "ping":
			if err := c.sendJSON(map[string]string{"t": "pong"}); err != nil {
				readErr = err
				c.closeAbort()
			}
		case "close":
			explicitClose = true
		case "debug":
			// 页面以 ?debug 打开时上报的键盘事件，排查输入法问题用
			if len(msg.D) > 400 {
				msg.D = msg.D[:400]
			}
			log.Printf("[debug] %s", msg.D)
		}
		if explicitClose {
			break
		}
	}

	c.closeAbort()
	wg.Wait()
	if explicitClose {
		s.terminate()
		return
	}
	s.detach(c, readErr)
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	if !originAllowed(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
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
		id := q.Get("target")
		if s == nil || (id != "" && s.targetID != id) {
			_ = c.sendJSON(map[string]string{"t": "error", "code": "session_expired"})
			c.closeNormal()
			return
		}
		since, _ := strconv.ParseUint(q.Get("since"), 10, 64)
		s.attach(c, since, cols, rows, true)
		return
	}

	id := q.Get("target")
	target, ok := findTarget(id)
	if !ok {
		_ = c.sendJSON(map[string]string{"t": "error", "code": "unknown_target", "detail": id})
		c.closeNormal()
		return
	}
	s, err := startPTYSession(target, cols, rows)
	if err != nil {
		_ = c.sendJSON(map[string]string{"t": "error", "code": "spawn_failed", "detail": err.Error()})
		c.closeNormal()
		return
	}
	s.attach(c, 0, cols, rows, false)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func exitInfo(ps *os.ProcessState) (code, signal int) {
	if ps == nil {
		return -1, 0
	}
	var ws syscall.WaitStatus
	if st, ok := ps.Sys().(syscall.WaitStatus); ok {
		ws = st
	} else {
		return ps.ExitCode(), 0
	}
	if ws.Signaled() {
		return 128 + int(ws.Signal()), int(ws.Signal())
	}
	return ws.ExitStatus(), 0
}
