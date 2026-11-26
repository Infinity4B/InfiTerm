// InfiTerm：本地网页终端。单个二进制，网页与 zsh 集成脚本都编译在里面。
//
//	infiterm [-port 7681]            启动服务
//	infiterm install [-port 7681]    注册为 launchd 用户服务（登录后自动启动），并立即启动
//	infiterm uninstall               注销 launchd 服务
//	infiterm version                 显示版本
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

var version = "dev"

//go:embed web
var webFS embed.FS

//go:embed all:shell
var shellFS embed.FS

// 解压后的 zsh 集成目录，作为本地 zsh 会话的 ZDOTDIR
var zshDir string

func itoa(n int) string { return strconv.Itoa(n) }

func main() {
	log.SetFlags(log.LstdFlags)
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && (args[0] == "serve" || args[0] == "install" || args[0] == "uninstall" || args[0] == "version") {
		cmd, args = args[0], args[1:]
	}
	if cmd == "version" {
		fmt.Println("InfiTerm", version)
		return
	}

	fl := flag.NewFlagSet(cmd, flag.ExitOnError)
	port := fl.Int("port", envInt("INFITERM_PORT", 7681), "监听端口")
	host := fl.String("host", "127.0.0.1", "监听地址，只允许本机回环地址")
	fl.Parse(args)

	var err error
	switch cmd {
	case "install":
		err = installAgent(*port)
	case "uninstall":
		err = uninstallAgent()
	default:
		err = serve(*host, *port)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "infiterm:", err)
		os.Exit(1)
	}
}

func envInt(key string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return n
	}
	return def
}

// zsh 只能从磁盘读启动文件，每次启动时把内置脚本写到应用数据目录
func extractShellFiles() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(base, "infiterm", "shell")
	err = fs.WalkDir(shellFS, "shell", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(root, p[len("shell"):])
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := shellFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	return filepath.Join(root, "zsh"), err
}
