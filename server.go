package main

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"path"
	"strings"
)

var allowedHostnames = map[string]bool{"localhost": true, "127.0.0.1": true, "[::1]": true}

var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".mjs":  "text/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".svg":  "image/svg+xml",
	".png":  "image/png",
}

func serve(host string, port int) error {
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return errors.New("出于安全考虑只允许监听本机回环地址")
	}
	dir, err := extractShellFiles()
	if err != nil {
		log.Printf("[zsh] 无法写出集成脚本，输入框补全不可用: %v", err)
	} else {
		zshDir = dir
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/targets", handleTargets)
	mux.HandleFunc("/api/fonts", handleFonts)
	mux.HandleFunc("/ws", handleWS)
	mux.HandleFunc("/", handleStatic)

	addr := net.JoinHostPort(host, itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("InfiTerm 已启动: http://localhost:%d", port)
	return http.Serve(ln, guard(mux))
}

// 防 DNS 重绑定：Host 头必须是本机地址
func hostAllowed(r *http.Request) bool {
	h := r.Host
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.HasSuffix(h, "]") {
		h = h[:i]
	}
	return allowedHostnames[h]
}

// 防跨站 WebSocket 劫持：任意网页都能连 ws://localhost，必须校验 Origin
func originAllowed(r *http.Request) bool {
	return hostAllowed(r) && r.Header.Get("Origin") == "http://"+r.Host
}

func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		if !hostAllowed(r) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleTargets(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(loadTargets())
}

func handleStatic(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if p == "/" {
		p = "/index.html"
	}
	data, err := webFS.ReadFile("web" + path.Clean(p))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct, ok := contentTypes[path.Ext(p)]
	if !ok {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Write(data)
}
