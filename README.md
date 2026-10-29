# Porthole

本地网页终端：Warp 风格的底部输入框、多标签、一键 SSH，用浏览器书签打开。

Go 写的单个二进制，网页（含 xterm.js）和 zsh 集成脚本都编译在里面，运行时不依赖 Node。

## 安装

```sh
make install      # 编译到 ~/.local/bin/porthole，注册 launchd：登录后自动启动、崩溃自动重启
make uninstall    # 注销服务并删除二进制
```

改了代码之后再执行一次 `make install`，会用新二进制重启服务。日志在 `~/Library/Logs/porthole.log`。

临时手动运行：`make run`（端口默认 7681，`make run PORT=7690` 可改）。

## 代码结构

| 文件 | 内容 |
|---|---|
| `main.go` | 入口，`serve` / `install` / `uninstall` 子命令，内嵌资源 |
| `server.go` | HTTP、静态文件、Host/Origin 校验 |
| `session.go` | WebSocket ↔ pty 会话 |
| `targets.go` | 目标列表（`~/.ssh/config`、`targets.json`）与启动命令 |
| `fonts.go` | 扫描本机字体目录，解析字体文件得到族名和是否等宽（`/api/fonts`） |
| `launchd.go` | launchd 服务注册 |
| `shell/zsh/` | zsh 集成，启动时写到 `~/Library/Application Support/porthole/shell/zsh` |
| `web/` | 前端；`web/vendor/` 是 @xterm/xterm 6.0.0、addon-fit 0.11.0、addon-web-links 0.12.0、addon-webgl 0.19.0 |

## 书签

| 书签地址 | 效果 |
|---|---|
| `http://localhost:7681/?t=local` | 本地 shell |
| `http://localhost:7681/?t=lab` | `ssh lab` |
| `http://localhost:7681/?t=local&t=lab&t=h100` | 一次打开三个标签 |

打开任意组合后按 `⌘D`，地址栏里已经是当前这组标签。

## 目标来源

- `local`：内置，本地登录 shell
- `~/.ssh/config` 中所有不含通配符的 `Host`（支持 `Include`）
- `~/.config/porthole/targets.json`：自定义目标，格式见 `targets.example.json`
  - `{"id", "name", "cwd"}`：在指定目录打开本地 shell
  - `{"id", "name", "ssh", "port"}`：SSH 目标
  - `{"id", "name", "command": [...]}`：运行指定命令，例如 tmux 会话

URL 里只能写目标 id，不能写任意命令。

## 输入框

默认收起，键盘直接输入终端。`` Ctrl+` `` 或右上角的键盘图标打开；打开后焦点在终端时按 `` Ctrl+` `` 回到输入框，在输入框里再按一次就收起。设置里可以改成打开页面时就显示。

| 按键 | 作用 |
|---|---|
| `Enter` / `Shift+Enter` | 执行 / 换行（多行内容按粘贴处理） |
| `↑` `↓` | 历史记录，按已输入内容前缀过滤，每个目标单独保存 |
| `Tab` / `Shift+Tab` | 调用 shell 自己的补全，结果回填到输入框（见下文 zsh 集成） |
| `→` / `End`（光标在行尾） | 接受灰色的 zsh-autosuggestions 提示 |
| `Ctrl+C` | 停在 zsh 提示符时原地清空当前输入（Warp 风格，不另起一行）；命令运行中发送中断。终端里同样如此 |
| `Ctrl+D` `Ctrl+Z` `Ctrl+L` | 发给终端（`Ctrl+D`/`Ctrl+Z` 只在输入框为空时） |
| `Esc`（输入框为空时） | 焦点切到终端，输入框保持打开 |
| `` Ctrl+` `` | 打开 / 回到 / 收起输入框 |

- vim、top、less 等全屏程序运行时，键盘自动直通终端，程序退出后回到输入框
- 检测到 `Password:` / `passphrase` / `密码` 这类提示时，输入内容不显示、不记入历史
- 中文输入法开启时 Ctrl 组合键和 Esc 同样有效；如果某个输入法下仍有按键失灵，在地址后加 `&debug` 打开，右上角会显示键盘事件明细

## zsh 集成

本地 zsh 会话会自动加载 `shell/zsh/porthole.zsh`（通过 `ZDOTDIR` 注入，你的 `.zshenv`/`.zprofile`/`.zshrc`/`.zlogin` 照常加载，不需要改动）。之后输入框和 zsh 的编辑缓冲区（ZLE）双向同步：

- 在输入框里打字，终端提示符那一行同步显示，zsh-autosuggestions、语法高亮照常工作
- `Tab` 由 zsh 自己补全（compinit、你的 zstyle 配置都生效），补全结果回填到输入框；候选列表显示在终端里
- 灰色提示显示在输入框里
- 命令运行期间在输入框里提前输入的内容，回到提示符后自动交给 shell

SSH 目标和非 zsh 的 shell 没有这层集成：`Tab` 会把已输入内容交给 shell 补全并临时切到终端，回车后回到输入框。

## 设置

右上角齿轮图标或 `Ctrl+,`：字体、语言、外观、终端字号、界面与输入框字号（标签栏、对话框等界面文字跟输入框同一字号）、行高、字间距、字重、光标样式与闪烁、回滚行数、Option 作为 Meta、打开页面时是否显示输入框。修改即时生效，保存在浏览器本地。

- **字体**：搜索框加下拉单选。列表是服务端扫描本机字体目录（含苹方等按需下载的系统字体）得到的，等宽字体排在前面并标「等宽」，每项用自身字体预览。输入关键字过滤，`↑↓` 选择，`Enter` 确定，`Esc` 收起。图标符号和中文由后备字体（Symbols Nerd Font、苹方）补齐
- **语言**：跟随系统 / 中文 / English，默认跟随浏览器语言。界面文字在 `web/i18n.js`
- **外观**：默认跟随系统的白天/夜间模式；右上角的外观图标（半圆 / 太阳 / 月亮）点击在「跟随系统 → 白天 → 夜间」之间循环

## 全局快捷键

`⌘K` 新建终端（弹出目标列表，回车打开，再按一次关闭；`Ctrl+Shift+T` 同样可用） · `Ctrl+Shift+W` 关闭 · `Ctrl+1..9` 切换 · `Ctrl+Shift+[ / ]` 上一个/下一个 · `` Ctrl+` `` 输入框 · `Ctrl+,` 设置

## 安全

- 只监听 `127.0.0.1`
- 校验 `Host` 头，防 DNS 重绑定
- WebSocket 校验 `Origin`，其他网页无法连接
- 禁止被 iframe 嵌入
- SSH 目标名不能以 `-` 开头，参数不经过 shell 拼接

## 已知限制

- 刷新或关闭标签页时会话随之结束（整个进程组先收到 SIGHUP，3 秒后 SIGKILL）；需要保留会话的话，用 `command` 目标套一层 `tmux new -A -s 名字`
- 补全候选列表显示在终端里，不是输入框下方的弹出菜单
- 输入框历史（↑↓）是 porthole 自己记录的，和 zsh 的 history 分开
