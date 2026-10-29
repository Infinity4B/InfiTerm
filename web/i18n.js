// 界面文案。HTML 里用 data-i18n（文本）、data-i18n-html、data-i18n-title、data-i18n-placeholder 标注，JS 里用 t()
const STRINGS = {
  zh: {
    'target.local': '本地',
    'btn.new': '新建终端 (⌘K)',
    'btn.composer': '显示/收起输入框 (Ctrl+`)',
    'btn.settings': '设置 (Ctrl+,)',
    'btn.appearance': '外观：{mode}（点击切换）',
    'appearance.system': '跟随系统',
    'appearance.light': '白天',
    'appearance.dark': '夜间',
    'home.hint': '点击打开目标；把链接拖到书签栏即可一键打开。打开多个标签后按 <kbd>⌘D</kbd>，书签会保存整组标签。',
    'target.linkTitle': '点击打开；拖到书签栏可保存为书签',
    'tab.close': '关闭 (Ctrl+Shift+W)',
    'composer.placeholder': '输入命令，Enter 执行 · Shift+Enter 换行 · ↑↓ 历史 · Tab 补全 · → 接受灰色提示 · Ctrl+` 收起',
    'composer.secretPlaceholder': '输入密码后按 Enter',
    'mode.closed': '已断开：按 Enter 重新连接',
    'mode.connecting': '连接中…',
    'mode.altScreen': '全屏程序运行中，键盘直接输入终端 · Ctrl+` 回到输入框',
    'mode.secret': '检测到密码提示：输入内容不显示、不记入历史',
    'mode.direct': '直通模式：键盘直接输入终端 · Ctrl+` 回到输入框',
    'term.exited': '[进程已退出，代码 {code}] 按 Enter 重新连接',
    'term.disconnected': '[连接已断开] 按 Enter 重新连接',
    'term.reconnecting': '──── 重新连接 ────',
    'term.unknownTarget': '未知目标: {detail}',
    'term.spawnFailed': '启动失败: {detail}',
    'picker.placeholder': '搜索目标…',
    'settings.title': '设置',
    'settings.font': '字体',
    'settings.fontSearch': '搜索字体…',
    'settings.fontMissing': '未安装 {font}，正在使用后备字体',
    'settings.fontCount': '共 {n} 个字体，等宽字体排在前面',
    'settings.fontLoadFailed': '读取字体列表失败',
    'settings.fontNoMatch': '没有匹配的字体',
    'settings.mono': '等宽',
    'settings.fontSize': '字号',
    'settings.uiFontSize': '界面与输入框字号',
    'settings.lineHeight': '行高',
    'settings.letterSpacing': '字间距',
    'settings.fontWeight': '字重',
    'weight.300': '细',
    'weight.normal': '常规',
    'weight.500': '中等',
    'weight.600': '半粗',
    'weight.bold': '粗',
    'settings.appearance': '外观',
    'settings.cursorStyle': '光标',
    'cursor.block': '方块',
    'cursor.bar': '竖线',
    'cursor.underline': '下划线',
    'settings.scrollback': '回滚行数',
    'settings.language': '语言',
    'language.auto': '跟随系统',
    'settings.cursorBlink': '光标闪烁',
    'settings.macOptionIsMeta': 'Option 键作为 Meta（Alt）',
    'settings.showComposer': '打开页面时显示输入框（随时可用 Ctrl+` 切换）',
    'settings.reset': '恢复默认',
    'settings.done': '完成',
  },
  en: {
    'target.local': 'Local',
    'btn.new': 'New terminal (⌘K)',
    'btn.composer': 'Show/hide input box (Ctrl+`)',
    'btn.settings': 'Settings (Ctrl+,)',
    'btn.appearance': 'Appearance: {mode} (click to switch)',
    'appearance.system': 'System',
    'appearance.light': 'Light',
    'appearance.dark': 'Dark',
    'home.hint': 'Click a target to open it, or drag its link to the bookmarks bar for one-click access. With several tabs open, press <kbd>⌘D</kbd> to bookmark the whole set.',
    'target.linkTitle': 'Click to open; drag to the bookmarks bar to save it',
    'tab.close': 'Close (Ctrl+Shift+W)',
    'composer.placeholder': 'Type a command · Enter run · Shift+Enter newline · ↑↓ history · Tab complete · → accept suggestion · Ctrl+` hide',
    'composer.secretPlaceholder': 'Type the password and press Enter',
    'mode.closed': 'Disconnected: press Enter to reconnect',
    'mode.connecting': 'Connecting…',
    'mode.altScreen': 'Full-screen program running, keys go straight to the terminal · Ctrl+` back to input box',
    'mode.secret': 'Password prompt detected: input is hidden and not saved to history',
    'mode.direct': 'Direct mode: keys go straight to the terminal · Ctrl+` back to input box',
    'term.exited': '[Process exited with code {code}] Press Enter to reconnect',
    'term.disconnected': '[Disconnected] Press Enter to reconnect',
    'term.reconnecting': '──── Reconnecting ────',
    'term.unknownTarget': 'Unknown target: {detail}',
    'term.spawnFailed': 'Failed to start: {detail}',
    'picker.placeholder': 'Search targets…',
    'settings.title': 'Settings',
    'settings.font': 'Font',
    'settings.fontSearch': 'Search fonts…',
    'settings.fontMissing': '{font} is not installed; using a fallback font',
    'settings.fontCount': '{n} fonts, monospace first',
    'settings.fontLoadFailed': 'Could not load the font list',
    'settings.fontNoMatch': 'No matching fonts',
    'settings.mono': 'mono',
    'settings.fontSize': 'Font size',
    'settings.uiFontSize': 'UI & input box font size',
    'settings.lineHeight': 'Line height',
    'settings.letterSpacing': 'Letter spacing',
    'settings.fontWeight': 'Font weight',
    'weight.300': 'Light',
    'weight.normal': 'Regular',
    'weight.500': 'Medium',
    'weight.600': 'Semibold',
    'weight.bold': 'Bold',
    'settings.appearance': 'Appearance',
    'settings.cursorStyle': 'Cursor',
    'cursor.block': 'Block',
    'cursor.bar': 'Bar',
    'cursor.underline': 'Underline',
    'settings.scrollback': 'Scrollback lines',
    'settings.language': 'Language',
    'language.auto': 'System',
    'settings.cursorBlink': 'Blinking cursor',
    'settings.macOptionIsMeta': 'Use Option as Meta (Alt)',
    'settings.showComposer': 'Show the input box when the page opens (toggle anytime with Ctrl+`)',
    'settings.reset': 'Reset to defaults',
    'settings.done': 'Done',
  },
};

let lang = 'zh';

export function resolveLanguage(pref) {
  if (pref === 'zh' || pref === 'en') return pref;
  return (navigator.languages?.[0] || navigator.language || '').toLowerCase().startsWith('zh') ? 'zh' : 'en';
}

export function setLanguage(l) {
  lang = STRINGS[l] ? l : 'en';
  document.documentElement.lang = lang === 'zh' ? 'zh-CN' : 'en';
}

export function t(key, vars = {}) {
  const s = STRINGS[lang][key] ?? STRINGS.zh[key] ?? key;
  return s.replace(/\{(\w+)\}/g, (_, k) => (k in vars ? String(vars[k]) : `{${k}}`));
}

export function translatePage(root = document) {
  for (const el of root.querySelectorAll('[data-i18n]')) el.textContent = t(el.dataset.i18n);
  for (const el of root.querySelectorAll('[data-i18n-html]')) el.innerHTML = t(el.dataset.i18nHtml);
  for (const el of root.querySelectorAll('[data-i18n-title]')) el.title = t(el.dataset.i18nTitle);
  for (const el of root.querySelectorAll('[data-i18n-placeholder]')) el.placeholder = t(el.dataset.i18nPlaceholder);
}
