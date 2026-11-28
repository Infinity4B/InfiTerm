package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"text/template"
	"time"
)

// 单元测试绝不能调用真实 launchctl：默认执行器直接让测试失败
func TestMain(m *testing.M) {
	launchctlRun = func(args ...string) (int, string, error) {
		panic("unit test called real launchctl: " + strings.Join(args, " "))
	}
	sleepFn = func(time.Duration) { panic("unit test called real sleep") }
	os.Exit(m.Run())
}

// fakeLaunchd 按子命令返回预设退出码；队列耗尽后一直返回最后一个值
type fakeLaunchd struct {
	t         *testing.T
	codes     map[string][]int
	outs      map[string]string
	calls     []string
	sleeps    []time.Duration
	now       time.Time
	onCommand func(sub string)
}

func newFakeLaunchd(t *testing.T) *fakeLaunchd {
	t.Helper()
	f := &fakeLaunchd{
		t:     t,
		codes: map[string][]int{},
		outs:  map[string]string{},
		now:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	oldRun, oldSleep, oldNow := launchctlRun, sleepFn, nowFn
	launchctlRun = f.run
	sleepFn = func(d time.Duration) {
		f.sleeps = append(f.sleeps, d)
		f.now = f.now.Add(d)
	}
	nowFn = func() time.Time { return f.now }
	t.Cleanup(func() { launchctlRun, sleepFn, nowFn = oldRun, oldSleep, oldNow })
	return f
}

func (f *fakeLaunchd) set(sub string, codes ...int) { f.codes[sub] = codes }

func (f *fakeLaunchd) run(args ...string) (int, string, error) {
	sub := args[0]
	f.calls = append(f.calls, strings.Join(args, " "))
	if f.onCommand != nil {
		f.onCommand(sub)
	}
	q, ok := f.codes[sub]
	if !ok || len(q) == 0 {
		f.t.Fatalf("unexpected launchctl %s", strings.Join(args, " "))
	}
	code := q[0]
	if len(q) > 1 {
		f.codes[sub] = q[1:]
	}
	out := f.outs[sub]
	if out == "" && code != 0 {
		out = fmt.Sprintf("fake %s failed: %d", sub, code)
	}
	return code, out, nil
}

func (f *fakeLaunchd) count(sub string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, sub+" ") {
			n++
		}
	}
	return n
}

func (f *fakeLaunchd) totalSleep() time.Duration {
	var d time.Duration
	for _, s := range f.sleeps {
		d += s
	}
	return d
}

func testConfig(t *testing.T) agentConfig {
	t.Helper()
	dir := t.TempDir()
	return agentConfig{
		Label: "dev.infiterm.unit",
		Plist: filepath.Join(dir, "LaunchAgents", "dev.infiterm.unit.plist"),
		Log:   filepath.Join(dir, "Logs", "infiterm.log"),
		Bin:   "/opt/infiterm/bin/infiterm",
		Port:  7681,
		UID:   501,
	}
}

const (
	svc    = "gui/501/dev.infiterm.unit"
	domain = "gui/501"
)

func assertCalls(t *testing.T, f *fakeLaunchd, want ...string) {
	t.Helper()
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("launchctl calls:\n%s\nwant:\n%s", strings.Join(f.calls, "\n"), strings.Join(want, "\n"))
	}
}

func assertOnlyFile(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != filepath.Base(path) {
		t.Fatalf("directory entries = %v, want only %s", names, filepath.Base(path))
	}
}

func writeOldPlist(t *testing.T, c agentConfig) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(c.Plist), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.Plist, []byte("old plist"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 场景 1：首次安装，bootout 返回 3 → 不 print，直接写 plist 并 bootstrap
func TestInstallFirstInstallSkipsWait(t *testing.T) {
	f := newFakeLaunchd(t)
	c := testConfig(t)
	f.set("bootout", rcNoSuchProcess)
	f.set("bootstrap", 0)
	f.onCommand = func(sub string) {
		if sub == "bootstrap" && !strings.Contains(readFile(t, c.Plist), "<string>7681</string>") {
			t.Fatal("plist not written before bootstrap")
		}
	}
	if err := installAgentConfig(c); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, f, "bootout "+svc, "bootstrap "+domain+" "+c.Plist)
	if len(f.sleeps) != 0 {
		t.Fatalf("first install slept %v", f.sleeps)
	}
	assertOnlyFile(t, c.Plist)
}

// 场景 2：运行中重启，bootout 异步返回 0 → print 直到 113 后才写 plist 和 bootstrap
func TestInstallWaitsForAsyncBootout(t *testing.T) {
	for _, rc := range []int{0, rcInProgress} {
		t.Run(fmt.Sprintf("bootout_rc_%d", rc), func(t *testing.T) {
			f := newFakeLaunchd(t)
			c := testConfig(t)
			writeOldPlist(t, c)
			f.set("bootout", rc)
			f.set("print", 0, 0, 0, rcServiceNotFound)
			f.set("bootstrap", 0)
			f.onCommand = func(sub string) {
				got := readFile(t, c.Plist)
				if sub == "print" && got != "old plist" {
					t.Fatal("plist replaced before old service was unloaded")
				}
				if sub == "bootstrap" && f.count("print") != 4 {
					t.Fatal("bootstrap called before print returned 113")
				}
			}
			if err := installAgentConfig(c); err != nil {
				t.Fatal(err)
			}
			assertCalls(t, f,
				"bootout "+svc, "print "+svc, "print "+svc, "print "+svc, "print "+svc,
				"bootstrap "+domain+" "+c.Plist)
			want := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond}
			if fmt.Sprint(f.sleeps) != fmt.Sprint(want) {
				t.Fatalf("poll sleeps = %v, want %v", f.sleeps, want)
			}
		})
	}
}

// 场景 3：卸载超时 → 明确超时错误，绝不 bootstrap，也不改写 plist
func TestInstallBootoutTimeoutNeverBootstraps(t *testing.T) {
	if bootoutTimeout <= 5*time.Second {
		t.Fatalf("bootoutTimeout %v must exceed launchd ExitTimeOut 5s", bootoutTimeout)
	}
	f := newFakeLaunchd(t)
	c := testConfig(t)
	writeOldPlist(t, c)
	f.set("bootout", 0)
	f.set("print", 0)
	err := installAgentConfig(c)
	if err == nil || !strings.Contains(err.Error(), "超时") || !strings.Contains(err.Error(), "launchctl print "+svc) {
		t.Fatalf("err = %v, want clear timeout error", err)
	}
	if f.count("bootstrap") != 0 {
		t.Fatal("bootstrap called after timeout")
	}
	if got := f.totalSleep(); got < bootoutTimeout || got > bootoutTimeout+pollIntervalMax {
		t.Fatalf("waited %v, want about %v", got, bootoutTimeout)
	}
	for _, s := range f.sleeps {
		if s > pollIntervalMax {
			t.Fatalf("poll interval %v exceeds %v", s, pollIntervalMax)
		}
	}
	if readFile(t, c.Plist) != "old plist" {
		t.Fatal("plist changed after timeout")
	}
}

// 场景 4：瞬态 bootstrap exit 5 → 在重试上限内恢复
func TestInstallBootstrapTransientExit5Retries(t *testing.T) {
	f := newFakeLaunchd(t)
	c := testConfig(t)
	f.set("bootout", 0)
	f.set("print", rcServiceNotFound)
	f.set("bootstrap", rcBootstrapIOError, 0)
	if err := installAgentConfig(c); err != nil {
		t.Fatal(err)
	}
	if n := f.count("bootstrap"); n != 2 {
		t.Fatalf("bootstrap calls = %d, want 2", n)
	}
	if fmt.Sprint(f.sleeps) != fmt.Sprint([]time.Duration{100 * time.Millisecond}) {
		t.Fatalf("sleeps = %v", f.sleeps)
	}
}

// 场景 5：非瞬态 bootstrap exit 5 → 有限次重试后报错，提示旧服务已停并给出恢复办法
func TestInstallBootstrapPersistentExit5FailsClearly(t *testing.T) {
	f := newFakeLaunchd(t)
	c := testConfig(t)
	f.set("bootout", 0)
	f.set("print", rcServiceNotFound)
	f.set("bootstrap", rcBootstrapIOError)
	f.outs["bootstrap"] = "Bootstrap failed: 5: Input/output error"
	err := installAgentConfig(c)
	if err == nil {
		t.Fatal("want error")
	}
	if n := f.count("bootstrap"); n < 3 || n > 4 {
		t.Fatalf("bootstrap attempts = %d, want 1 + 2..3 retries", n)
	}
	if d := f.totalSleep(); d >= time.Second {
		t.Fatalf("retry backoff total %v, want < 1s", d)
	}
	for _, want := range []string{
		"旧服务已停止", "7681",
		"launchctl bootstrap " + domain + " " + c.Plist, "exit status 5", "Bootstrap failed: 5: Input/output error",
		"infiterm install -port 7681", "launchctl print-disabled " + domain, c.Label,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
	// 新 plist 已原子写入，便于用户直接重试
	if !strings.Contains(readFile(t, c.Plist), "<string>7681</string>") {
		t.Fatal("plist not written")
	}
}

func TestInstallBootstrapOtherErrorDoesNotRetry(t *testing.T) {
	f := newFakeLaunchd(t)
	c := testConfig(t)
	f.set("bootout", rcNoSuchProcess)
	f.set("bootstrap", 1)
	err := installAgentConfig(c)
	if err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "旧服务已停止") {
		t.Fatalf("first install must not claim an old service was stopped: %v", err)
	}
	if f.count("bootstrap") != 1 || len(f.sleeps) != 0 {
		t.Fatalf("calls = %v sleeps = %v", f.calls, f.sleeps)
	}
}

// 场景 6：非预期的 bootout / print 错误 → 立即报错，不误判为未安装/已卸载
func TestInstallUnexpectedBootoutError(t *testing.T) {
	f := newFakeLaunchd(t)
	c := testConfig(t)
	writeOldPlist(t, c)
	f.set("bootout", 1)
	err := installAgentConfig(c)
	if err == nil || !strings.Contains(err.Error(), "launchctl bootout "+svc+": exit status 1") {
		t.Fatalf("err = %v", err)
	}
	assertCalls(t, f, "bootout "+svc)
	if readFile(t, c.Plist) != "old plist" {
		t.Fatal("plist changed after bootout failure")
	}
}

func TestInstallUnexpectedPrintError(t *testing.T) {
	f := newFakeLaunchd(t)
	c := testConfig(t)
	f.set("bootout", 0)
	f.set("print", 0, 112)
	err := installAgentConfig(c)
	if err == nil || !strings.Contains(err.Error(), "launchctl print "+svc+": exit status 112") {
		t.Fatalf("err = %v", err)
	}
	assertCalls(t, f, "bootout "+svc, "print "+svc, "print "+svc)
}

// 场景 8：模板渲染失败时不触发 bootout，旧 plist 不变
func TestInstallTemplateFailureBeforeStop(t *testing.T) {
	f := newFakeLaunchd(t)
	c := testConfig(t)
	writeOldPlist(t, c)
	old := plistTmpl
	plistTmpl = template.Must(template.New("broken").Parse(`{{template "missing"}}`))
	t.Cleanup(func() { plistTmpl = old })
	if err := installAgentConfig(c); err == nil {
		t.Fatal("want template error")
	}
	if len(f.calls) != 0 {
		t.Fatalf("launchctl called after template failure: %v", f.calls)
	}
	if readFile(t, c.Plist) != "old plist" {
		t.Fatal("plist changed after template failure")
	}
}

func TestInstallInvalidXMLBeforeStop(t *testing.T) {
	f := newFakeLaunchd(t)
	c := testConfig(t)
	old := plistTmpl
	plistTmpl = template.Must(template.New("bad").Parse(`<plist><dict></plist>`))
	t.Cleanup(func() { plistTmpl = old })
	if err := installAgentConfig(c); err == nil || !strings.Contains(err.Error(), "XML") {
		t.Fatalf("err = %v, want XML validation error", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("launchctl called after invalid plist: %v", f.calls)
	}
}

// 场景 8：原子替换 plist，内容正确且无临时文件残留
func TestInstallReplacesPlistAtomically(t *testing.T) {
	f := newFakeLaunchd(t)
	c := testConfig(t)
	c.Bin = "/Users/a&b/<bin>/infiterm"
	writeOldPlist(t, c)
	f.set("bootout", rcNoSuchProcess)
	f.set("bootstrap", 0)
	if err := installAgentConfig(c); err != nil {
		t.Fatal(err)
	}
	data := readFile(t, c.Plist)
	var p struct {
		Strings []string `xml:"dict>string"`
		Args    []string `xml:"dict>array>string"`
	}
	if err := xml.Unmarshal([]byte(data), &p); err != nil {
		t.Fatalf("plist is not valid XML: %v\n%s", err, data)
	}
	wantArgs := []string{c.Bin, "serve", "-port", "7681"}
	if fmt.Sprint(p.Args) != fmt.Sprint(wantArgs) {
		t.Fatalf("ProgramArguments = %q, want %q", p.Args, wantArgs)
	}
	if fmt.Sprint(p.Strings) != fmt.Sprint([]string{c.Label, c.Log, c.Log}) {
		t.Fatalf("strings = %q", p.Strings)
	}
	st, err := os.Stat(c.Plist)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Fatalf("plist mode = %v, want 0644", st.Mode().Perm())
	}
	assertOnlyFile(t, c.Plist)
}

func TestWriteFileAtomicFailureLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "x.plist")
	if err := os.Mkdir(target, 0o755); err != nil { // rename 到非空目录必然失败
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(target, []byte("new"), 0o644); err == nil {
		t.Fatal("want rename error")
	}
	assertOnlyFile(t, target)
}

// 场景 7：uninstall 已安装 → 等卸载完成再删除 plist
func TestUninstallWaitsThenRemovesPlist(t *testing.T) {
	f := newFakeLaunchd(t)
	c := testConfig(t)
	writeOldPlist(t, c)
	f.set("bootout", 0)
	f.set("print", 0, rcServiceNotFound)
	f.onCommand = func(sub string) {
		if _, err := os.Stat(c.Plist); err != nil {
			t.Fatalf("plist removed before unload finished (during %s)", sub)
		}
	}
	if err := uninstallAgentConfig(c); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, f, "bootout "+svc, "print "+svc, "print "+svc)
	if _, err := os.Stat(c.Plist); !os.IsNotExist(err) {
		t.Fatalf("plist still exists: %v", err)
	}
}

// 场景 7：uninstall 未安装（exit 3）幂等成功，重复执行也成功
func TestUninstallNotLoadedIsIdempotent(t *testing.T) {
	f := newFakeLaunchd(t)
	c := testConfig(t)
	writeOldPlist(t, c)
	f.set("bootout", rcNoSuchProcess)
	for i := 0; i < 2; i++ {
		if err := uninstallAgentConfig(c); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	assertCalls(t, f, "bootout "+svc, "bootout "+svc)
	if _, err := os.Stat(c.Plist); !os.IsNotExist(err) {
		t.Fatalf("plist still exists: %v", err)
	}
}

func TestUninstallFailureKeepsPlist(t *testing.T) {
	for name, setup := range map[string]func(f *fakeLaunchd){
		"bootout_error": func(f *fakeLaunchd) { f.set("bootout", 1) },
		"print_error":   func(f *fakeLaunchd) { f.set("bootout", 0); f.set("print", 112) },
		"timeout":       func(f *fakeLaunchd) { f.set("bootout", 0); f.set("print", 0) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeLaunchd(t)
			c := testConfig(t)
			writeOldPlist(t, c)
			setup(f)
			if err := uninstallAgentConfig(c); err == nil {
				t.Fatal("want error")
			}
			if readFile(t, c.Plist) != "old plist" {
				t.Fatal("plist removed although unload failed")
			}
		})
	}
}

func TestRunCommandReportsExitCodeAndOutput(t *testing.T) {
	code, out, err := runCommand("sh", "-c", "echo out; echo err >&2; exit 113")
	if err != nil || code != 113 || !strings.Contains(out, "out") || !strings.Contains(out, "err") {
		t.Fatalf("code=%d out=%q err=%v", code, out, err)
	}
	if code, _, err := runCommand("sh", "-c", "exit 0"); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if _, _, err := runCommand(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("want exec error for missing binary")
	}
}

// ---- macOS 隔离集成测试 ----
//
// 默认跳过。INFITERM_LAUNCHD_INTEGRATION=1 go test -run TestLaunchdIntegration -v
// 只使用临时 label dev.infiterm.test.<pid>、临时 plist/日志/HOME 和随机高位端口，
// 绝不触碰正在运行的 dev.infiterm 服务及 ~/Library/LaunchAgents/dev.infiterm.plist。

func TestLaunchdIntegration(t *testing.T) {
	if os.Getenv("INFITERM_LAUNCHD_INTEGRATION") != "1" {
		t.Skip("set INFITERM_LAUNCHD_INTEGRATION=1 to run isolated launchd integration test")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("launchd only")
	}
	oldRun, oldSleep, oldNow := launchctlRun, sleepFn, nowFn
	launchctlRun, sleepFn, nowFn = runLaunchctl, time.Sleep, time.Now
	t.Cleanup(func() { launchctlRun, sleepFn, nowFn = oldRun, oldSleep, oldNow })

	dir := t.TempDir()
	label := fmt.Sprintf("dev.infiterm.test.%d", os.Getpid())
	realPlist, _ := agentPaths()
	c := agentConfig{
		Label: label,
		Plist: filepath.Join(dir, "LaunchAgents", label+".plist"),
		Log:   filepath.Join(dir, "Logs", "infiterm.log"),
		UID:   os.Getuid(),
	}
	if c.Label == agentLabel || c.Plist == realPlist {
		t.Fatal("refusing to touch the real dev.infiterm service")
	}
	t.Cleanup(func() {
		launchctlRun("bootout", c.service())
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if code, _, _ := launchctlRun("print", c.service()); code == rcServiceNotFound {
				t.Logf("cleanup: launchctl print %s -> 113", c.service())
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Errorf("cleanup: %s still loaded", c.service())
	})

	t.Run("slow_sigterm_exit_reinstall_20x", func(t *testing.T) {
		script := filepath.Join(dir, "slow-exit.sh")
		body := "#!/bin/sh\ntrap 'sleep 0.3; exit 0' TERM\nwhile :; do sleep 1 & wait $!; done\n"
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		c := c
		c.Bin, c.Port = script, freePort(t)
		var pids []string
		for i := 0; i < 20; i++ {
			start := time.Now()
			if err := installAgentConfig(c); err != nil {
				t.Fatalf("reinstall %d: %v", i+1, err)
			}
			pid := waitPID(t, c, pids)
			pids = append(pids, pid)
			t.Logf("reinstall %2d ok in %v pid=%s", i+1, time.Since(start).Round(time.Millisecond), pid)
		}
		if err := uninstallAgentConfig(c); err != nil {
			t.Fatal(err)
		}
	})

	bin := filepath.Join(dir, "infiterm")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	// 隔离 HOME，避免测试实例改写真实服务的 zsh 集成目录与 targets 配置
	home := filepath.Join(dir, "home")
	wrapper := filepath.Join(dir, "infiterm-isolated.sh")
	wrapperBody := fmt.Sprintf("#!/bin/sh\nexport HOME='%s'\nexec '%s' \"$@\"\n", home, bin)
	if err := os.WriteFile(wrapper, []byte(wrapperBody), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("infiterm_reinstall_20x", func(t *testing.T) {
		c := c
		c.Bin, c.Port = wrapper, freePort(t)
		var pids []string
		for i := 0; i < 20; i++ {
			start := time.Now()
			if err := installAgentConfig(c); err != nil {
				t.Fatalf("reinstall %d: %v", i+1, err)
			}
			pid := waitPID(t, c, pids)
			pids = append(pids, pid)
			waitHTTP(t, c.Port, true)
			t.Logf("reinstall %2d ok in %v pid=%s port=%d responding", i+1, time.Since(start).Round(time.Millisecond), pid, c.Port)
		}

		portB := freePort(t)
		portA := c.Port
		c.Port = portB
		if err := installAgentConfig(c); err != nil {
			t.Fatalf("change port: %v", err)
		}
		waitHTTP(t, portB, true)
		waitHTTP(t, portA, false)
		t.Logf("port change %d -> %d ok: new port responding, old port closed", portA, portB)

		if err := uninstallAgentConfig(c); err != nil {
			t.Fatal(err)
		}
		if err := uninstallAgentConfig(c); err != nil {
			t.Fatalf("second uninstall must be idempotent: %v", err)
		}
		if code, _, _ := launchctlRun("print", c.service()); code != rcServiceNotFound {
			t.Fatalf("launchctl print after uninstall = %d, want 113", code)
		}
		waitHTTP(t, portB, false)
		if entries, _ := os.ReadDir(filepath.Dir(c.Plist)); len(entries) != 0 {
			t.Fatalf("leftover files in plist dir: %v", entries)
		}
		t.Logf("uninstall ok: print -> 113, port %d closed, plist dir empty", portB)
	})
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	if port == 7681 {
		t.Fatal("got owner port 7681")
	}
	return port
}

var pidRe = regexp.MustCompile(`(?m)^\s*pid = (\d+)`)

// 等待新进程启动，并确认 PID 与之前每一次都不同
func waitPID(t *testing.T, c agentConfig, seen []string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, out, _ := launchctlRun("print", c.service())
		if m := pidRe.FindStringSubmatch(out); m != nil {
			for _, s := range seen {
				if s == m[1] {
					t.Fatalf("pid %s reused, service was not restarted", s)
				}
			}
			return m[1]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s has no running pid", c.service())
	return ""
}

func waitHTTP(t *testing.T, port int, up bool) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err == nil {
			resp.Body.Close()
		}
		if (err == nil && resp.StatusCode < 500) == up {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("port %d responding=%v never reached", port, !up)
}

var _ = bytes.NewReader
