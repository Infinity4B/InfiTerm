package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

const agentLabel = "dev.porthole"

var plistTmpl = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>{{.Label}}</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{.Bin}}</string>
    <string>serve</string>
    <string>-port</string>
    <string>{{.Port}}</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>{{.Log}}</string>
  <key>StandardErrorPath</key><string>{{.Log}}</string>
</dict>
</plist>
`))

func agentPaths() (plist, logFile string) {
	home := homeDir()
	return filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist"),
		filepath.Join(home, "Library", "Logs", "porthole.log")
}

func launchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
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
	plist, logFile := agentPaths()
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return err
	}
	f, err := os.Create(plist)
	if err != nil {
		return err
	}
	err = plistTmpl.Execute(f, map[string]any{"Label": agentLabel, "Bin": bin, "Port": port, "Log": logFile})
	f.Close()
	if err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = launchctl("bootout", domain+"/"+agentLabel)
	if err := launchctl("bootstrap", domain, plist); err != nil {
		return err
	}
	fmt.Printf("已安装并启动：http://localhost:%d\n二进制：%s\n日志：%s\n", port, bin, logFile)
	return nil
}

func uninstallAgent() error {
	plist, _ := agentPaths()
	_ = launchctl("bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), agentLabel))
	if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("已卸载", agentLabel)
	return nil
}
