package main

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	idRe = regexp.MustCompile(`^[A-Za-z0-9._@-]+$`)
	// ssh 目标只允许常规主机名字符，且不能以 "-" 开头，防止被当成 ssh 选项（如 -oProxyCommand=...）
	sshDestRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._@:%\[\]-]*$`)
)

type Target struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Source  string   `json:"source"`
	Detail  string   `json:"detail"`
	SSH     string   `json:"-"`
	Port    int      `json:"-"`
	Command []string `json:"-"`
	Cwd     string   `json:"-"`
}

type SpawnSpec struct {
	File string
	Args []string
	Cwd  string
	Env  []string
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "/"
	}
	return h
}

func targetsFile() string {
	return filepath.Join(homeDir(), ".config", "infiterm", "targets.json")
}

func expandHome(p string) string {
	if p == "~" {
		return homeDir()
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(homeDir(), p[2:])
	}
	return p
}

func readSSHConfigHosts(file string, seen map[string]bool) []string {
	if seen[file] {
		return nil
	}
	seen[file] = true
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()

	var hosts []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, _ := strings.Cut(strings.Replace(line, "=", " ", 1), " ")
		value = strings.TrimSpace(value)
		switch strings.ToLower(key) {
		case "include":
			for _, pattern := range strings.Fields(value) {
				p := expandHome(pattern)
				if !filepath.IsAbs(p) {
					p = filepath.Join(homeDir(), ".ssh", p)
				}
				files, _ := filepath.Glob(p)
				for _, inc := range files {
					hosts = append(hosts, readSSHConfigHosts(inc, seen)...)
				}
			}
		case "host":
			for _, h := range strings.Fields(value) {
				if strings.ContainsAny(h, "*?!") || !sshDestRe.MatchString(h) {
					continue
				}
				hosts = append(hosts, h)
			}
		}
	}
	return hosts
}

type customTarget struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	SSH     string          `json:"ssh"`
	Port    int             `json:"port"`
	Command json.RawMessage `json:"command"`
	Cwd     string          `json:"cwd"`
}

func readTargetsFile() []customTarget {
	data, err := os.ReadFile(targetsFile())
	if err != nil {
		return nil
	}
	var list []customTarget
	if err := json.Unmarshal(data, &list); err == nil {
		return list
	}
	var wrapped struct {
		Targets []customTarget `json:"targets"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		log.Printf("[targets] 无法解析 %s: %v", targetsFile(), err)
		return nil
	}
	return wrapped.Targets
}

func normalizeCustom(t customTarget) (Target, bool) {
	if !idRe.MatchString(t.ID) {
		return Target{}, false
	}
	base := Target{ID: t.ID, Name: t.Name, Source: "targets.json"}
	if base.Name == "" {
		base.Name = t.ID
	}
	if t.SSH != "" {
		if !sshDestRe.MatchString(t.SSH) {
			return Target{}, false
		}
		base.Kind, base.SSH = "ssh", t.SSH
		if t.Port > 0 && t.Port < 65536 {
			base.Port = t.Port
		}
		return base, true
	}
	var cmd []string
	if len(t.Command) > 0 && json.Unmarshal(t.Command, &cmd) == nil && len(cmd) > 0 {
		base.Kind, base.Command, base.Cwd = "command", cmd, t.Cwd
		return base, true
	}
	base.Kind, base.Cwd = "local", t.Cwd
	return base, true
}

func withDetail(t Target) Target {
	switch t.Kind {
	case "ssh":
		t.Detail = t.SSH
		if t.Port > 0 {
			t.Detail += ":" + itoa(t.Port)
		}
	case "command":
		t.Detail = strings.Join(t.Command, " ")
	default:
		t.Detail = t.Cwd
		if t.Detail == "" {
			t.Detail = "~"
		}
	}
	return t
}

// 每次请求时重新读取，修改配置后刷新页面即可生效
func loadTargets() []Target {
	list := []Target{withDetail(Target{ID: "local", Name: "本地", Kind: "local", Source: "builtin"})}
	ids := map[string]bool{"local": true}
	for _, ct := range readTargetsFile() {
		if t, ok := normalizeCustom(ct); ok && !ids[t.ID] {
			ids[t.ID] = true
			list = append(list, withDetail(t))
		}
	}
	for _, h := range readSSHConfigHosts(filepath.Join(homeDir(), ".ssh", "config"), map[string]bool{}) {
		if ids[h] {
			continue
		}
		ids[h] = true
		list = append(list, withDetail(Target{ID: h, Name: h, Kind: "ssh", SSH: h, Source: "ssh_config"}))
	}
	return list
}

func findTarget(id string) (Target, bool) {
	for _, t := range loadTargets() {
		if t.ID == id {
			return t, true
		}
	}
	return Target{}, false
}

// launchd 启动时环境变量里不一定有 SHELL，从目录服务查用户的登录 shell
func userShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if u, err := user.Current(); err == nil {
		out, err := exec.Command("/usr/bin/dscl", ".", "-read", "/Users/"+u.Username, "UserShell").Output()
		if err == nil {
			if _, s, ok := strings.Cut(strings.TrimSpace(string(out)), ": "); ok && s != "" {
				return s
			}
		}
	}
	return "/bin/zsh"
}

func spawnSpec(t Target) SpawnSpec {
	cwd := homeDir()
	if t.Cwd != "" {
		cwd = expandHome(t.Cwd)
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		cwd = homeDir()
	}
	switch t.Kind {
	case "ssh":
		ssh := "/usr/bin/ssh"
		if _, err := os.Stat(ssh); err != nil {
			ssh = "ssh"
		}
		args := []string{"--", t.SSH}
		if t.Port > 0 {
			args = append([]string{"-p", itoa(t.Port)}, args...)
		}
		return SpawnSpec{File: ssh, Args: args, Cwd: homeDir()}
	case "command":
		// 经登录 shell 启动以获得完整 PATH（如 /opt/homebrew/bin），参数原样传递，不做 shell 拼接
		args := append([]string{"-l", "-c", `exec "$0" "$@"`}, t.Command...)
		return SpawnSpec{File: userShell(), Args: args, Cwd: cwd}
	default:
		// 登录 shell：launchd 启动时 PATH 很精简，-l 会加载用户的完整环境
		shell := userShell()
		spec := SpawnSpec{File: shell, Args: []string{"-l"}, Cwd: cwd}
		if filepath.Base(shell) == "zsh" && zshDir != "" {
			// 经 ZDOTDIR 注入输入框集成，用户自己的配置照常加载，见 shell/zsh/.zshenv
			userZdotdir := os.Getenv("ZDOTDIR")
			if userZdotdir == "" {
				userZdotdir = homeDir()
			}
			spec.Env = []string{"INFITERM_USER_ZDOTDIR=" + userZdotdir, "ZDOTDIR=" + zshDir}
		}
		return spec
	}
}
