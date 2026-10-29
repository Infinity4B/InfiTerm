package main

import (
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

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
	CheckOrigin:     originAllowed,
}

// 浏览器 → 服务端：二进制帧或 {"t":"in","d":...} 是输入，{"t":"resize","cols","rows"} 调整尺寸
// 服务端 → 浏览器：二进制帧是终端输出，文本帧是 {"t":"ready"|"exit"|"error"}
type controlMsg struct {
	T    string `json:"t"`
	D    string `json:"d"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

type session struct {
	ws *websocket.Conn
	mu sync.Mutex // gorilla/websocket 不允许并发写
}

func (s *session) sendJSON(v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ws.WriteJSON(v)
}

func (s *session) sendBinary(b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ws.WriteMessage(websocket.BinaryMessage, b)
}

func (s *session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	s.ws.Close()
}

func clampSize(v, def, max int) uint16 {
	if v > 0 && v <= max {
		return uint16(v)
	}
	return uint16(def)
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
	s := &session{ws: ws}
	q := r.URL.Query()
	id := q.Get("target")
	target, ok := findTarget(id)
	if !ok {
		s.sendJSON(map[string]string{"t": "error", "code": "unknown_target", "detail": id})
		s.close()
		return
	}
	cols, _ := strconv.Atoi(q.Get("cols"))
	rows, _ := strconv.Atoi(q.Get("rows"))
	runSession(s, target, clampSize(cols, 80, 1000), clampSize(rows, 24, 500))
}

func runSession(s *session, target Target, cols, rows uint16) {
	spec := spawnSpec(target)
	cmd := exec.Command(spec.File, spec.Args...)
	cmd.Dir = spec.Cwd
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=porthole")
	if os.Getenv("LANG") == "" {
		cmd.Env = append(cmd.Env, "LANG=en_US.UTF-8")
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		s.sendJSON(map[string]string{"t": "error", "code": "spawn_failed", "detail": err.Error()})
		s.close()
		return
	}
	defer ptmx.Close()
	pid := cmd.Process.Pid
	log.Printf("[session] open %s pid=%d", target.ID, pid)
	s.sendJSON(map[string]any{"t": "ready", "pid": pid})

	// pty → 浏览器。WriteMessage 会阻塞到发出为止，浏览器来不及消费时自然暂停读取 pty。
	// 连接断开后也要继续读并丢弃：macOS 上进程退出时要等 tty 输出排空，没人读的话会卡在退出状态，连 SIGKILL 都收不了尾
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		buf := make([]byte, 32*1024)
		connected := true
		for {
			n, err := ptmx.Read(buf)
			if n > 0 && connected {
				connected = s.sendBinary(buf[:n]) == nil
			}
			if err != nil {
				return
			}
		}
	}()

	exited := make(chan struct{})
	go func() {
		defer close(exited)
		cmd.Wait()
		// 把进程退出前的最后一段输出发完；后台子进程仍占着 pty 时不无限等待
		select {
		case <-outputDone:
		case <-time.After(300 * time.Millisecond):
		}
		code, signal := exitInfo(cmd.ProcessState)
		log.Printf("[session] exit %s pid=%d code=%d", target.ID, pid, code)
		s.sendJSON(map[string]any{"t": "exit", "code": code, "signal": signal})
		s.close()
	}()

	// 浏览器 → pty，直到连接断开
	for {
		mt, data, err := s.ws.ReadMessage()
		if err != nil {
			break
		}
		if mt == websocket.BinaryMessage {
			ptmx.Write(data)
			continue
		}
		var msg controlMsg
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.T {
		case "in":
			ptmx.Write([]byte(msg.D))
		case "resize":
			pty.Setsize(ptmx, &pty.Winsize{Cols: clampSize(msg.Cols, 80, 1000), Rows: clampSize(msg.Rows, 24, 500)})
		case "debug":
			// 页面以 ?debug 打开时上报的键盘事件，排查输入法问题用
			if len(msg.D) > 400 {
				msg.D = msg.D[:400]
			}
			log.Printf("[debug] %s", msg.D)
		}
	}

	// 页面关闭或刷新：先 SIGHUP 整个进程组（和关闭终端窗口一样），3 秒内没退出再强杀
	select {
	case <-exited:
		return
	default:
	}
	syscall.Kill(-pid, syscall.SIGHUP)
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		syscall.Kill(-pid, syscall.SIGKILL)
		<-exited
	}
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
