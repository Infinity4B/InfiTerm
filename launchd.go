package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

const agentLabel = "dev.infiterm"

// launchctl 退出码。man launchctl 声明 print 的文本格式不是稳定接口，所以只按退出码判断。
const (
	rcBootstrapIOError = 5   // Bootstrap failed: 5: Input/output error（同 label 尚未完全卸载等）
	rcNoSuchProcess    = 3   // bootout：服务未加载
	rcInProgress       = 36  // bootout：EINPROGRESS，卸载进行中（旧系统）
	rcServiceNotFound  = 113 // print：Could not find service，已从 domain 中移除
)

var (
	// 包级可替换钩子，单元测试用 mock 替换，避免真的调用 launchctl 或等待
	launchctlRun = runLaunchctl
	sleepFn      = time.Sleep
	nowFn        = time.Now

	// 必须大于 launchd 默认 ExitTimeOut（5s）：旧进程卡住时 launchd 会在 5s 后强杀回收
	bootoutTimeout     = 10 * time.Second
	pollIntervalStart  = 50 * time.Millisecond
	pollIntervalMax    = 200 * time.Millisecond
	bootstrapRetryWait = []time.Duration{100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond}
)

var plistTmpl = template.Must(template.New("plist").Funcs(template.FuncMap{"xml": xmlEscape}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>{{xml .Label}}</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{xml .Bin}}</string>
    <string>serve</string>
    <string>-port</string>
    <string>{{.Port}}</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>{{xml .Log}}</string>
  <key>StandardErrorPath</key><string>{{xml .Log}}</string>
</dict>
</plist>
`))

func xmlEscape(s string) (string, error) {
	var b strings.Builder
	err := xml.EscapeText(&b, []byte(s))
	return b.String(), err
}

// agentConfig 描述一个 LaunchAgent；正式安装用 defaultAgentConfig，测试可指向临时 label 与目录
type agentConfig struct {
	Label string
	Plist string
	Log   string
	Bin   string
	Port  int
	UID   int
}

func (c agentConfig) domain() string  { return fmt.Sprintf("gui/%d", c.UID) }
func (c agentConfig) service() string { return c.domain() + "/" + c.Label }

func agentPaths() (plist, logFile string) {
	home := homeDir()
	return filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist"),
		filepath.Join(home, "Library", "Logs", "infiterm.log")
}

func defaultAgentConfig(port int) agentConfig {
	plist, logFile := agentPaths()
	return agentConfig{Label: agentLabel, Plist: plist, Log: logFile, Port: port, UID: os.Getuid()}
}

// runLaunchctl 返回退出码与合并输出；err 只表示 launchctl 本身无法执行
func runCommand(name string, args ...string) (int, string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), text, nil
	}
	if err != nil {
		return -1, text, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return 0, text, nil
}

func runLaunchctl(args ...string) (int, string, error) {
	return runCommand("launchctl", args...)
}

func launchctlError(code int, out string, args ...string) error {
	msg := fmt.Sprintf("launchctl %s: exit status %d", strings.Join(args, " "), code)
	if out != "" {
		msg += ": " + out
	}
	return errors.New(msg)
}

// renderPlist 在内存中渲染并校验 plist，任何错误都发生在停止旧服务之前
func renderPlist(c agentConfig) ([]byte, error) {
	var buf bytes.Buffer
	if err := plistTmpl.Execute(&buf, c); err != nil {
		return nil, fmt.Errorf("生成 plist 失败: %w", err)
	}
	dec := xml.NewDecoder(bytes.NewReader(buf.Bytes()))
	for {
		if _, err := dec.Token(); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("生成的 plist 不是合法 XML: %w", err)
		}
	}
	return buf.Bytes(), nil
}

// bootoutAndWait 卸载服务并等到它真正从 domain 中消失。
// bootout 是异步的：返回时旧进程可能还在 SIGTERMed 状态，此时 bootstrap 同 label 会失败（exit 5）。
// stopped 表示确实停掉了一个已加载的服务。
func bootoutAndWait(domain, label string, timeout time.Duration) (stopped bool, err error) {
	service := domain + "/" + label
	code, out, err := launchctlRun("bootout", service)
	if err != nil {
		return false, err
	}
	switch code {
	case rcNoSuchProcess:
		return false, nil
	case 0, rcInProgress:
	default:
		return false, launchctlError(code, out, "bootout", service)
	}

	deadline := nowFn().Add(timeout)
	interval := pollIntervalStart
	for {
		code, out, err := launchctlRun("print", service)
		if err != nil {
			return true, err
		}
		switch code {
		case rcServiceNotFound:
			return true, nil
		case 0:
		default:
			return true, launchctlError(code, out, "print", service)
		}
		if !nowFn().Before(deadline) {
			return true, fmt.Errorf("等待 %s 卸载超时（%s），旧服务仍在退出；请稍后用 launchctl print %s 确认已卸载后重试", label, timeout, service)
		}
		sleepFn(interval)
		if interval *= 2; interval > pollIntervalMax {
			interval = pollIntervalMax
		}
	}
}

// bootstrapWithRetry 只对 exit 5 做有限次短退避重试：非瞬态错误（被 disable、plist 异常等）同样返回 5，不能无限重试
func bootstrapWithRetry(domain, plist string) error {
	for i := 0; ; i++ {
		code, out, err := launchctlRun("bootstrap", domain, plist)
		if err != nil {
			return err
		}
		if code == 0 {
			return nil
		}
		if code != rcBootstrapIOError || i >= len(bootstrapRetryWait) {
			return launchctlError(code, out, "bootstrap", domain, plist)
		}
		sleepFn(bootstrapRetryWait[i])
	}
}

// writeFileAtomic 先写同目录临时文件再 rename，中途失败不会留下空的或残缺的 plist
func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// 注册为登录后自动启动、崩溃后自动重启的用户服务；重复执行会用当前二进制重启服务
func installAgent(port int) error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if bin, err = filepath.EvalSymlinks(bin); err != nil {
		return err
	}
	c := defaultAgentConfig(port)
	c.Bin = bin
	if err := installAgentConfig(c); err != nil {
		return err
	}
	fmt.Printf("已安装并启动：http://localhost:%d\n二进制：%s\n日志：%s\n", port, bin, c.Log)
	return nil
}

func installAgentConfig(c agentConfig) error {
	data, err := renderPlist(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.Plist), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.Log), 0o755); err != nil {
		return err
	}
	stopped, err := bootoutAndWait(c.domain(), c.Label, bootoutTimeout)
	if err != nil {
		if stopped {
			return fmt.Errorf("已请求停止旧服务（端口 %d 可能已无人监听），但未能确认其卸载完成，未启动新服务：%w\n确认卸载后可重新执行 infiterm install -port %d", c.Port, err, c.Port)
		}
		return err
	}
	if err := writeFileAtomic(c.Plist, data, 0o644); err != nil {
		return startFailure(c, stopped, fmt.Errorf("写入 %s 失败: %w", c.Plist, err))
	}
	if err := bootstrapWithRetry(c.domain(), c.Plist); err != nil {
		return startFailure(c, stopped, err)
	}
	return nil
}

func startFailure(c agentConfig, stopped bool, err error) error {
	prefix := "新服务启动失败"
	if stopped {
		prefix = fmt.Sprintf("旧服务已停止（端口 %d 当前无人监听），但新服务启动失败", c.Port)
	}
	return fmt.Errorf("%s：%w\n可重新执行 infiterm install -port %d 重试；若仍失败，请检查 launchctl print-disabled %s 中 %s 是否被禁用",
		prefix, err, c.Port, c.domain(), c.Label)
}

func uninstallAgent() error {
	c := defaultAgentConfig(0)
	if err := uninstallAgentConfig(c); err != nil {
		return err
	}
	fmt.Println("已卸载", c.Label)
	return nil
}

// 等服务真正卸载后才删除 plist；服务未加载（exit 3）视为成功，重复执行幂等
func uninstallAgentConfig(c agentConfig) error {
	if _, err := bootoutAndWait(c.domain(), c.Label, bootoutTimeout); err != nil {
		return err
	}
	if err := os.Remove(c.Plist); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
