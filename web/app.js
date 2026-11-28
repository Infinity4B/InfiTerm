import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import { WebglAddon } from '@xterm/addon-webgl';
import { t, setLanguage, resolveLanguage, translatePage } from './i18n.js';
import { sshHostFromCommand, isSSHCommand } from './tab-title.js';

const $ = (s) => document.querySelector(s);
const tabsEl = $('#tabs');
const panesEl = $('#panes');
const input = $('#input');
const ghost = $('#ghost');
const ghostTyped = $('#ghost-typed');
const ghostHint = $('#ghost-hint');
const metaTarget = $('#meta-target');
const metaMode = $('#meta-mode');
const picker = $('#picker');
const pickerFilter = $('#picker-filter');
const pickerList = $('#picker-list');
const settingsDialog = $('#settings');
const settingsForm = $('#settings-form');
const btnAppearance = $('#btn-appearance');
const btnComposer = $('#btn-composer');
const fontInput = $('#font-input');
const fontOptions = $('#font-options');

const HISTORY_MAX = 1000;
const REATTACH_MAX = 6;
const SESSIONS_KEY = 'infiterm:sessions';
const TAB_ID = typeof crypto?.randomUUID === 'function' ? crypto.randomUUID() : String(Math.random()).slice(2);
const SECRET_RE = /(password|passphrase|密码|口令|verification code|otp)[^\n]{0,160}[:：]\s*$/i;
const KIND_LABEL = { local: 'local', ssh: 'ssh', command: 'cmd' };

const THEMES = {
  dark: {
    ui: {
      bg: '#15171c', 'bg-2': '#1c1f26', 'bg-3': '#252932', fg: '#d7dae0', muted: '#7d8590', placeholder: '#4d5460',
      accent: '#5aa9ff', warn: '#f0b04a', danger: '#f2626b', ok: '#4fc27a', border: '#2c313b',
    },
    term: {
      background: '#15171c', foreground: '#d7dae0', cursor: '#5aa9ff', cursorAccent: '#15171c', selectionBackground: '#3a4a66',
      black: '#1c1f26', red: '#f2626b', green: '#4fc27a', yellow: '#f0b04a', blue: '#5aa9ff', magenta: '#c678dd', cyan: '#56b6c2', white: '#d7dae0',
      brightBlack: '#5c6370', brightRed: '#ff7a85', brightGreen: '#6fdc96', brightYellow: '#ffc861', brightBlue: '#7dbcff', brightMagenta: '#d891ef', brightCyan: '#6fd0dc', brightWhite: '#ffffff',
    },
  },
  light: {
    ui: {
      bg: '#fafafa', 'bg-2': '#f0f0f1', 'bg-3': '#e3e4e7', fg: '#383a42', muted: '#80858f', placeholder: '#a8acb3',
      accent: '#3b75e8', warn: '#b07800', danger: '#d9473d', ok: '#3f9a3f', border: '#dadbdf',
    },
    term: {
      background: '#fafafa', foreground: '#383a42', cursor: '#3b75e8', cursorAccent: '#fafafa', selectionBackground: '#cfdcf5',
      black: '#383a42', red: '#e45649', green: '#50a14f', yellow: '#c18401', blue: '#4078f2', magenta: '#a626a4', cyan: '#0184bc', white: '#a0a1a7',
      brightBlack: '#696c77', brightRed: '#ef6b5f', brightGreen: '#5fb85e', brightYellow: '#d19a14', brightBlue: '#5a8df5', brightMagenta: '#bc4dba', brightCyan: '#1ba0d6', brightWhite: '#202227',
    },
  },
};

/* ---------- 设置 ---------- */

const SETTINGS_KEY = 'infiterm:settings';
const DEFAULT_SETTINGS = {
  fontFamily: 'Maple Mono NF CN',
  language: 'auto',
  fontSize: 13,
  uiFontSize: 14,
  lineHeight: 1.15,
  letterSpacing: 0,
  fontWeight: 'normal',
  appearance: 'system',
  cursorStyle: 'block',
  scrollback: 10000,
  cursorBlink: true,
  macOptionIsMeta: true,
  showComposer: false,
};
const NUMBER_RANGES = {
  fontSize: [8, 40],
  uiFontSize: [10, 32],
  lineHeight: [1, 2.5],
  letterSpacing: [-2, 10],
  scrollback: [1000, 200000],
};
const APPEARANCES = ['system', 'light', 'dark'];
const APPEARANCE_ICONS = {
  system: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor"/></svg>',
  light: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/></svg>',
  dark: '<svg viewBox="0 0 24 24"><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9z"/></svg>',
};
// 设置里只存所选字体的族名；图标符号和中文由后备字体补齐
const FONT_FALLBACK = '"Symbols Nerd Font Mono", "MesloLGS NF", Menlo, "PingFang SC", monospace';
const fontStack = (family) => `"${(family || DEFAULT_SETTINGS.fontFamily).replace(/["\\]/g, '')}", ${FONT_FALLBACK}`;
const darkQuery = matchMedia('(prefers-color-scheme: dark)');

let settings = loadSettings();

function loadSettings() {
  try {
    const saved = JSON.parse(localStorage.getItem(SETTINGS_KEY)) || {};
    // 旧版本的「输入框字号」现在同时决定界面字号；旧的固定主题改为跟随系统
    if (saved.uiFontSize == null && saved.composerFontSize != null) saved.uiFontSize = saved.composerFontSize;
    delete saved.composerFontSize;
    delete saved.theme;
    // 旧版本存的是整串 CSS 字体列表，取第一个族名
    if (typeof saved.fontFamily === 'string' && /[,"']/.test(saved.fontFamily)) {
      const first = saved.fontFamily.split(',')[0].trim().replace(/^["']|["']$/g, '');
      saved.fontFamily = first && !['monospace', 'serif', 'sans-serif'].includes(first) ? first : undefined;
    }
    if (!saved.fontFamily) delete saved.fontFamily;
    return { ...DEFAULT_SETTINGS, ...saved };
  } catch {
    return { ...DEFAULT_SETTINGS };
  }
}

function resolvedTheme() {
  const a = settings.appearance;
  if (a === 'light' || a === 'dark') return THEMES[a];
  return darkQuery.matches ? THEMES.dark : THEMES.light;
}

function termOptions() {
  return {
    fontFamily: fontStack(settings.fontFamily),
    fontSize: settings.fontSize,
    lineHeight: settings.lineHeight,
    letterSpacing: settings.letterSpacing,
    fontWeight: settings.fontWeight,
    fontWeightBold: 'bold',
    cursorStyle: settings.cursorStyle,
    cursorBlink: settings.cursorBlink,
    scrollback: settings.scrollback,
    macOptionIsMeta: settings.macOptionIsMeta,
    theme: resolvedTheme().term,
  };
}

function applySettings() {
  setLanguage(resolveLanguage(settings.language));
  translatePage();
  const theme = resolvedTheme();
  const root = document.documentElement.style;
  for (const [k, v] of Object.entries(theme.ui)) root.setProperty(`--${k}`, v);
  root.setProperty('--mono', fontStack(settings.fontFamily));
  root.setProperty('--ui-size', `${settings.uiFontSize}px`);
  root.colorScheme = theme === THEMES.light ? 'light' : 'dark';
  const mode = APPEARANCES.includes(settings.appearance) ? settings.appearance : 'system';
  btnAppearance.innerHTML = APPEARANCE_ICONS[mode];
  btnAppearance.title = t('btn.appearance', { mode: t(`appearance.${mode}`) });
  const opts = termOptions();
  for (const p of state.panes) {
    Object.assign(p.term.options, opts);
    p.fit();
  }
  autosize();
  updateFontStatus();
  localizeTargets();
}

// 内置目标的名字跟随界面语言；标签、标题、列表里用的都是同一个对象
function localizeTargets() {
  for (const tg of state.targets) if (tg.source === 'builtin' && tg.id === 'local') tg.name = t('target.local');
  renderTabs();
  renderTargetList($('#home-list'), '', -1, (tg) => openPane(tg.id));
  if (picker.open) refreshPicker();
  updateComposer();
  if (state.active) document.title = `${state.active.label} · InfiTerm`;
}

function fillSettingsForm() {
  for (const el of settingsForm.elements) {
    if (!el.name || !(el.name in settings)) continue;
    if (el.type === 'checkbox') el.checked = !!settings[el.name];
    else el.value = settings[el.name];
  }
  fontInput.value = settings.fontFamily;
  closeFontList(false);
  loadFontList();
}

function readSettingsForm() {
  const next = { ...settings };
  for (const el of settingsForm.elements) {
    if (!el.name || !(el.name in DEFAULT_SETTINGS)) continue;
    if (el.type === 'checkbox') next[el.name] = el.checked;
    else if (el.name in NUMBER_RANGES) {
      const n = Number(el.value);
      const [min, max] = NUMBER_RANGES[el.name];
      if (el.value !== '' && Number.isFinite(n)) next[el.name] = Math.min(max, Math.max(min, n));
    } else next[el.name] = el.value;
  }
  return next;
}

function saveSettings(next) {
  settings = next;
  localStorage.setItem(SETTINGS_KEY, JSON.stringify(settings));
  applySettings();
}

/* ---------- 字体选择：可搜索的单选列表，字体来自服务端扫描的本机字体（/api/fonts） ---------- */

let fontList = null;
let fontListFailed = false;
let fontFilter = '';
let fontMatches = [];
let fontIndex = -1;

async function loadFontList() {
  if (!fontList) {
    try {
      fontList = await (await fetch('/api/fonts')).json();
    } catch {
      fontList = [];
      fontListFailed = true;
    }
  }
  updateFontStatus();
}

function updateFontStatus() {
  const status = $('#font-status');
  let text = '';
  let warn = false;
  if (fontListFailed) {
    text = t('settings.fontLoadFailed');
    warn = true;
  } else if (fontList) {
    warn = !fontList.some((f) => f.family === settings.fontFamily);
    text = warn ? t('settings.fontMissing', { font: settings.fontFamily }) : t('settings.fontCount', { n: fontList.length });
  }
  status.textContent = text;
  status.dataset.kind = warn ? 'warn' : '';
}

function renderFontOptions() {
  const q = fontFilter.trim().toLowerCase();
  fontMatches = (fontList || []).filter((f) => !q || f.family.toLowerCase().includes(q));
  if (fontIndex >= fontMatches.length) fontIndex = fontMatches.length - 1;
  if (!fontMatches.length) {
    fontOptions.replaceChildren(Object.assign(document.createElement('div'), { className: 'combo-empty', textContent: t('settings.fontNoMatch') }));
    return;
  }
  fontOptions.replaceChildren(
    ...fontMatches.map((f, i) => {
      const opt = document.createElement('div');
      opt.className = 'combo-option';
      opt.id = `font-opt-${i}`;
      opt.setAttribute('role', 'option');
      opt.setAttribute('aria-selected', String(f.family === settings.fontFamily));
      const name = document.createElement('span');
      name.textContent = f.family;
      name.style.fontFamily = `"${f.family.replace(/["\\]/g, '')}", ${FONT_FALLBACK}`;
      opt.append(name);
      if (f.mono) opt.append(Object.assign(document.createElement('span'), { className: 'badge', textContent: t('settings.mono') }));
      // mousedown 时阻止默认行为，输入框不失焦，click 才能选中
      opt.addEventListener('mousedown', (e) => e.preventDefault());
      opt.addEventListener('click', () => chooseFont(f));
      opt.addEventListener('mousemove', () => {
        if (fontIndex !== i) highlightFont(i);
      });
      return opt;
    }),
  );
  highlightFont(fontIndex);
}

function highlightFont(i) {
  fontOptions.querySelector('.active')?.classList.remove('active');
  fontIndex = i;
  const el = i >= 0 ? fontOptions.children[i] : null;
  if (!el || !el.classList.contains('combo-option')) {
    fontInput.removeAttribute('aria-activedescendant');
    return;
  }
  el.classList.add('active');
  el.scrollIntoView({ block: 'nearest' });
  fontInput.setAttribute('aria-activedescendant', el.id);
}

async function openFontList(filter) {
  await loadFontList();
  fontFilter = filter;
  fontIndex = filter ? 0 : fontList.findIndex((f) => f.family === settings.fontFamily);
  fontOptions.hidden = false;
  fontInput.setAttribute('aria-expanded', 'true');
  renderFontOptions();
}

function closeFontList(revert) {
  fontOptions.hidden = true;
  fontInput.setAttribute('aria-expanded', 'false');
  fontInput.removeAttribute('aria-activedescendant');
  if (revert) fontInput.value = settings.fontFamily;
}

function chooseFont(f) {
  saveSettings({ ...settings, fontFamily: f.family });
  closeFontList(true);
}

fontInput.addEventListener('focus', () => fontInput.select());
fontInput.addEventListener('click', () => {
  if (!fontOptions.hidden) return;
  fontInput.select();
  openFontList('');
});
fontInput.addEventListener('input', () => openFontList(fontInput.value));
fontInput.addEventListener('blur', () => closeFontList(true));
fontInput.addEventListener('keydown', (e) => {
  if (e.isComposing || e.keyCode === 229) return;
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault();
    if (fontOptions.hidden) return openFontList('');
    const n = fontMatches.length;
    if (n) highlightFont((fontIndex + (e.key === 'ArrowDown' ? 1 : -1) + n) % n);
  } else if (e.key === 'Enter') {
    // 不让回车提交表单（会关闭设置窗口）
    e.preventDefault();
    if (!fontOptions.hidden && fontMatches[fontIndex]) chooseFont(fontMatches[fontIndex]);
  } else if (e.key === 'Escape' && !fontOptions.hidden) {
    // 只收起列表，不关闭设置窗口
    e.preventDefault();
    e.stopPropagation();
    closeFontList(true);
  }
});

function openSettings() {
  fillSettingsForm();
  settingsDialog.showModal();
}

settingsForm.addEventListener('input', (e) => {
  if (e.target !== fontInput) saveSettings(readSettingsForm());
});
settingsForm.addEventListener('change', (e) => {
  if (e.target !== fontInput) saveSettings(readSettingsForm());
});
$('#btn-reset').addEventListener('click', () => {
  saveSettings({ ...DEFAULT_SETTINGS });
  fillSettingsForm();
});
btnAppearance.addEventListener('click', () => {
  const i = APPEARANCES.indexOf(settings.appearance);
  saveSettings({ ...settings, appearance: APPEARANCES[(i + 1) % APPEARANCES.length] });
});
darkQuery.addEventListener('change', () => {
  if (settings.appearance === 'system') applySettings();
});
settingsDialog.addEventListener('close', () => {
  if (state.active) syncFocusWithBuffer();
});

/* ---------- 会话 ---------- */

const state = {
  targets: [],
  panes: [],
  active: null,
  composerOpen: false,
};
let paneSeq = 0;

function targetById(id) {
  return state.targets.find((t) => t.id === id) || { id, name: id, kind: '?', detail: '' };
}

const isPlainCtrl = (e) => e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey;
// 物理键位；个别输入法给的 code 为空时，从未被改写的 keyCode 推回来
const keyCodeOf = (e) => e.code || (e.keyCode >= 65 && e.keyCode <= 90 ? `Key${String.fromCharCode(e.keyCode)}` : '');
const ctrlLetter = (e) => {
  const code = keyCodeOf(e);
  return isPlainCtrl(e) && /^Key[A-Z]$/.test(code) ? code[3] : null;
};
const decodeField = (s) => {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
};
const cpLength = (s) => [...s].length;
const cpToIndex = (s, cp) => [...s].slice(0, cp).join('').length;

function newShellState() {
  // ready：zsh 正停在提示符等待输入（集成脚本报告），此时输入框与 ZLE 缓冲区双向同步
  return { ready: false, gen: 0, buffer: '', cursor: 0, ghost: '' };
}

let keepAliveRelease = null;

// 有活跃终端时持有当前标签页专属的 Web Lock，避免 Chromium FreezingPolicy 在切到其他标签页后冻结本页断开 WebSocket
function updateKeepAliveLock() {
  const need = state.panes.some((p) => p.state === 'open' || p.state === 'connecting' || p.state === 'reconnecting');
  if (need && !keepAliveRelease) {
    if (!navigator.locks?.request) return;
    let release;
    const held = new Promise((r) => {
      release = r;
    });
    keepAliveRelease = release;
    navigator.locks.request(`infiterm:keepalive:${TAB_ID}`, () => held).catch(() => {});
  } else if (!need && keepAliveRelease) {
    const release = keepAliveRelease;
    keepAliveRelease = null;
    release();
  }
}

function releaseKeepAliveLock() {
  if (!keepAliveRelease) return;
  const release = keepAliveRelease;
  keepAliveRelease = null;
  release();
}

function saveTabSessions() {
  try {
    const list = state.panes.map((p) => ({
      target: p.target.id,
      session: p.sessionId || null,
      remoteHost: p.remoteHost || null,
    }));
    if (list.some((x) => x.session)) sessionStorage.setItem(SESSIONS_KEY, JSON.stringify(list));
    else sessionStorage.removeItem(SESSIONS_KEY);
  } catch {}
}

function loadTabSessions() {
  // 仅在浏览器 Memory Saver 丢弃重载或页面刷新时恢复 sessionStorage 中的会话，避免复制标签页时抢占原标签页的会话
  const nav = performance.getEntriesByType?.('navigation')?.[0];
  if (!document.wasDiscarded && nav?.type !== 'reload') return [];
  try {
    const list = JSON.parse(sessionStorage.getItem(SESSIONS_KEY));
    return Array.isArray(list) ? list : [];
  } catch {
    return [];
  }
}

class Pane {
  constructor(target, { sessionId = null, remoteHost = '' } = {}) {
    this.id = ++paneSeq;
    this.target = target;
    this.state = 'connecting';
    this.secret = false;
    this.autoReturn = false;
    this.pending = [];
    this.ws = null;
    this.sessionId = sessionId;
    this.rxBytes = 0;
    this.disposed = false;
    this.reattachAttempt = 0;
    this.reattachTimer = null;
    this.probeTimer = null;
    this.draft = '';
    this.shell = newShellState();
    this.sshActive = false;
    this.remoteHost = sessionId ? remoteHost : '';

    this.el = document.createElement('div');
    this.el.className = 'pane';
    const termEl = document.createElement('div');
    termEl.className = 'term';
    this.el.append(termEl);
    panesEl.append(this.el);

    this.term = new Terminal({ ...termOptions(), macOptionClickForcesSelection: true });
    this.fitAddon = new FitAddon();
    this.term.loadAddon(this.fitAddon);
    this.term.loadAddon(new WebLinksAddon((_e, uri) => window.open(uri, '_blank', 'noopener')));
    this.term.open(termEl);
    // WebGL 渲染器自己绘制 powerline/方框字符，严丝合缝填满单元格；DOM 渲染器靠字体，Nerd Font 的圆角符号会被裁切。
    // 不支持 WebGL 或上下文丢失时退回 DOM 渲染器
    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => webgl.dispose());
      this.term.loadAddon(webgl);
    } catch {}

    this.term.parser.registerOscHandler(7777, (data) => {
      this.onShellMessage(data);
      return true;
    });
    this.term.onData((d) => this.onUserData(d));
    this.term.onResize(({ cols, rows }) => this.sendControl({ t: 'resize', cols, rows }));
    this.term.onWriteParsed(() => this.detectSecretPrompt());
    this.term.onTitleChange((title) => {
      this.title = title;
      renderTabs();
    });
    this.term.buffer.onBufferChange(() => {
      if (this === state.active) syncFocusWithBuffer();
      updateComposer();
    });
    this.term.textarea?.addEventListener('focus', () => setMode('direct'));

    this.resizeObserver = new ResizeObserver(() => this.fit());
    this.resizeObserver.observe(this.el);
  }

  get altScreen() {
    return this.term.buffer.active.type === 'alternate';
  }

  get atPrompt() {
    return this.state === 'open' && this.shell.ready && !this.altScreen;
  }

  fit() {
    if (!this.el.clientWidth || !this.el.clientHeight) return;
    try {
      this.fitAddon.fit();
    } catch {}
  }

  connect({ reattach = false, fallbackNew = false } = {}) {
    if (this.disposed) return;
    this.clearTimers();
    if (!reattach || !this.sessionId) {
      reattach = false;
      this.sessionId = null;
      this.rxBytes = 0;
      this.shell = newShellState();
      this.sshActive = false;
      this.remoteHost = '';
      this.setState('connecting');
    } else {
      this.setState('reconnecting');
    }
    // 先确定尺寸再启动 pty，否则 shell 按 80 列输出的提示符会错位；隐藏的标签沿用当前标签的尺寸
    this.fit();
    const ref = state.active;
    if (!this.el.clientWidth && ref && ref !== this) this.term.resize(ref.term.cols, ref.term.rows);
    const q = new URLSearchParams({ target: this.target.id, cols: this.term.cols, rows: this.term.rows });
    if (reattach) {
      q.set('session', this.sessionId);
      q.set('since', String(this.rxBytes));
    }
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${location.host}/ws?${q}`);
    ws.binaryType = 'arraybuffer';
    this.ws = ws;
    let exited = false;

    ws.onmessage = (ev) => {
      if (this.ws !== ws || this.disposed) return;
      this.clearProbeTimer();
      if (ev.data instanceof ArrayBuffer) {
        this.rxBytes += ev.data.byteLength;
        this.term.write(new Uint8Array(ev.data));
        return;
      }
      const msg = JSON.parse(ev.data);
      if (msg.t === 'ready') {
        this.reattachAttempt = 0;
        this.sessionId = msg.session || null;
        if (typeof msg.offset === 'number') this.rxBytes = msg.offset;
        if (!msg.reattached) {
          this.shell = newShellState();
          this.clearRemoteHost();
        }
        saveTabSessions();
        this.setState('open');
        this.sendControl({ t: 'resize', cols: this.term.cols, rows: this.term.rows });
        for (const d of this.pending.splice(0)) this.send(d);
      } else if (msg.t === 'pong') {
        // 活性探测响应，已清除 probeTimer
      } else if (msg.t === 'exit') {
        exited = true;
        this.sessionId = null;
        this.clearRemoteHost();
        saveTabSessions();
        this.term.write(`\r\n\x1b[90m${t('term.exited', { code: msg.code })}\x1b[0m\r\n`);
      } else if (msg.t === 'error') {
        if (msg.code === 'session_expired') {
          this.sessionId = null;
          saveTabSessions();
          if (fallbackNew) {
            this.ws = null;
            ws.close();
            this.connect({ reattach: false });
          }
          return;
        }
        exited = true;
        this.sessionId = null;
        saveTabSessions();
        const key = { unknown_target: 'term.unknownTarget', spawn_failed: 'term.spawnFailed' }[msg.code];
        this.term.write(`\r\n\x1b[31m${key ? t(key, { detail: msg.detail }) : msg.message || msg.code}\x1b[0m\r\n`);
      }
    };
    ws.onclose = () => {
      if (this.ws !== ws || this.disposed) return;
      this.clearProbeTimer();
      this.ws = null;
      if (!exited && this.sessionId) {
        this.scheduleReattach();
        return;
      }
      this.markClosed(!exited);
    };
  }

  markClosed(showDisconnected) {
    this.clearTimers();
    this.sessionId = null;
    if (showDisconnected) this.term.write(`\r\n\x1b[90m${t('term.disconnected')}\x1b[0m\r\n`);
    this.secret = false;
    this.shell = newShellState();
    this.clearRemoteHost();
    saveTabSessions();
    this.setState('closed');
    if (this === state.active) renderGhost();
  }

  clearProbeTimer() {
    if (this.probeTimer) {
      clearTimeout(this.probeTimer);
      this.probeTimer = null;
    }
  }

  clearTimers() {
    this.clearProbeTimer();
    if (this.reattachTimer) {
      clearTimeout(this.reattachTimer);
      this.reattachTimer = null;
    }
  }

  scheduleReattach() {
    if (this.disposed || !this.sessionId) return;
    this.clearTimers();
    this.setState('reconnecting');
    // 标签页在后台时不消耗重试次数，等切回前台或唤醒时立即重连
    if (document.hidden) return;
    if (this.reattachAttempt >= REATTACH_MAX) {
      this.markClosed(true);
      return;
    }
    const delay = Math.min(8000, 300 * 2 ** this.reattachAttempt);
    this.reattachAttempt++;
    this.reattachTimer = setTimeout(() => {
      this.reattachTimer = null;
      this.connect({ reattach: true });
    }, delay);
  }

  reattachNow() {
    if (this.disposed || !this.sessionId) return;
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) return;
    this.reattachAttempt = 0;
    this.connect({ reattach: true });
  }

  // 切回标签页或从休眠恢复时探测连接是否半开；若 3 秒内无响应则主动断开并触发无感重连
  probeConnection() {
    if (this.disposed) return;
    if (this.state === 'reconnecting') {
      this.reattachNow();
      return;
    }
    if (this.state !== 'open') return;
    const ws = this.ws;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      this.reattachNow();
      return;
    }
    this.clearProbeTimer();
    this.sendControl({ t: 'ping' });
    this.probeTimer = setTimeout(() => {
      this.probeTimer = null;
      if (this.ws === ws && !this.disposed) ws.close();
    }, 3000);
  }

  reconnect() {
    this.term.write(`\r\n\x1b[90m${t('term.reconnecting')}\x1b[0m\r\n`);
    this.connect({ reattach: false });
  }

  setState(s) {
    this.state = s;
    renderTabs();
    updateKeepAliveLock();
    if (this === state.active) updateComposer();
  }

  send(d) {
    if (!d.startsWith('\x1b[7777~')) debugLog(`→ pty ${describeText(d)}`);
    if (this.state === 'closed') {
      this.pending.push(d);
      this.reconnect();
    } else if (this.state === 'connecting' || this.state === 'reconnecting') {
      this.pending.push(d);
      if (this.state === 'reconnecting') this.reattachNow();
    } else {
      this.sendControl({ t: 'in', d });
    }
  }

  sendControl(msg) {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(msg));
  }

  onUserData(d) {
    this.send(d);
    // 没有 shell 集成时 Tab 会把键盘交给终端，回车或 Ctrl+C 结束这一行时回到输入框
    if (this.autoReturn && !this.altScreen && (d.includes('\r') || d === '\x03')) {
      this.autoReturn = false;
      setTimeout(focusComposer, 0);
    }
  }

  // 来自 shell/zsh/infiterm.zsh 的 OSC 7777
  onShellMessage(data) {
    const [kind, gen, cursor, buffer = '', hint = ''] = data.split(';');
    const sh = this.shell;
    const active = this === state.active;
    if (kind === 'ready') {
      this.clearRemoteHost();
      saveTabSessions();
      sh.ready = true;
      sh.buffer = '';
      sh.cursor = 0;
      sh.ghost = '';
      // 命令运行期间在输入框里提前敲好的内容，回到提示符后同步给 shell
      if (active && input.value) shellSync(this);
    } else if (kind === 'busy') {
      this.sshActive = this.target.kind === 'local' && isSSHCommand(sh.buffer);
      if (this.sshActive) {
        this.remoteHost = sshHostFromCommand(sh.buffer);
        renderTabs();
        if (this === state.active) document.title = `${this.label} · InfiTerm`;
        saveTabSessions();
      }
      sh.ready = false;
      sh.ghost = '';
      // shell 接受了这一行（回车执行或 Ctrl+C），输入框随之清空
      if (active) {
        if (input.value === sh.buffer) setComposerValue('');
      } else if (this.draft === sh.buffer) this.draft = '';
    } else if (kind === 'buf') {
      // 输入框之后又改过的话，这是过期的状态，丢弃
      if (Number(gen) !== sh.gen) return;
      sh.buffer = decodeField(buffer);
      sh.cursor = Number(cursor) || 0;
      sh.ghost = decodeField(hint);
      if (active) applyShellState(this);
      else this.draft = sh.buffer;
    }
    if (active) updateComposer();
  }

  get label() {
    return this.remoteHost || this.target.name;
  }

  clearRemoteHost() {
    this.sshActive = false;
    if (!this.remoteHost) return;
    this.remoteHost = '';
    renderTabs();
    if (this === state.active) document.title = `${this.label} · InfiTerm`;
  }

  detectSecretPrompt() {
    const buf = this.term.buffer.active;
    let secret = false;
    if (buf.type === 'normal' && !this.shell.ready) {
      const line = buf.getLine(buf.baseY + buf.cursorY);
      const text = line ? line.translateToString(true, 0, buf.cursorX) : '';
      secret = SECRET_RE.test(text);
    }
    if (secret !== this.secret) {
      this.secret = secret;
      if (this === state.active) updateComposer();
    }
  }

  dispose() {
    this.disposed = true;
    this.clearTimers();
    this.resizeObserver.disconnect();
    const ws = this.ws;
    this.ws = null;
    if (ws?.readyState === WebSocket.OPEN) {
      try {
        ws.send(JSON.stringify({ t: 'close' }));
      } catch {}
    }
    ws?.close();
    this.sessionId = null;
    this.term.dispose();
    this.el.remove();
  }
}

/* ---------- 标签 ---------- */

function openPane(targetId, { activateIt = true, sessionId = null, remoteHost = '' } = {}) {
  const pane = new Pane(targetById(targetId), { sessionId, remoteHost });
  state.panes.push(pane);
  if (activateIt || !state.active) activate(pane);
  else renderTabs();
  syncUrl();
  pane.connect({ reattach: !!sessionId, fallbackNew: !!sessionId });
  return pane;
}

function closePane(pane) {
  const i = state.panes.indexOf(pane);
  if (i < 0) return;
  pane.dispose();
  state.panes.splice(i, 1);
  if (state.active === pane) {
    state.active = null;
    const next = state.panes[i] || state.panes[i - 1];
    if (next) activate(next);
  }
  renderTabs();
  saveTabSessions();
  updateKeepAliveLock();
  syncUrl();
}

function activate(pane, { focus = true } = {}) {
  const prev = state.active;
  if (prev !== pane) {
    if (prev) prev.draft = input.value;
    state.active = pane;
    setComposerValue(pane.draft);
  }
  for (const p of state.panes) p.el.classList.toggle('active', p === pane);
  renderTabs();
  updateComposer();
  requestAnimationFrame(() => pane.fit());
  document.title = `${pane.label} · InfiTerm`;
  if (focus) syncFocusWithBuffer();
  syncUrl();
}

function renderTabs() {
  tabsEl.replaceChildren(
    ...state.panes.map((p, i) => {
      const tab = document.createElement('div');
      tab.className = 'tab';
      tab.setAttribute('role', 'tab');
      tab.setAttribute('aria-selected', String(p === state.active));
      tab.title = p.title || p.target.detail || p.target.id;
      const dot = document.createElement('span');
      dot.className = 'dot';
      dot.dataset.state = p.state;
      const idx = document.createElement('span');
      idx.className = 'idx';
      idx.textContent = i < 9 ? String(i + 1) : '';
      const label = document.createElement('span');
      label.className = 'label';
      label.textContent = p.label;
      const close = document.createElement('button');
      close.className = 'close';
      close.textContent = '×';
      close.title = t('tab.close');
      close.addEventListener('click', (e) => {
        e.stopPropagation();
        closePane(p);
      });
      tab.append(dot, idx, label, close);
      tab.addEventListener('click', () => activate(p));
      tab.addEventListener('auxclick', (e) => {
        if (e.button === 1) closePane(p);
      });
      return tab;
    }),
  );
  document.body.classList.toggle('empty', state.panes.length === 0);
  if (!state.panes.length) document.title = 'InfiTerm';
}

function syncUrl() {
  const q = new URLSearchParams();
  for (const p of state.panes) q.append('t', p.target.id);
  if (new URLSearchParams(location.search).has('debug')) q.set('debug', '');
  const s = q.toString();
  history.replaceState(null, '', s ? `/?${s}` : '/');
}

/* ---------- 输入框 ---------- */

function setMode(mode) {
  document.body.dataset.mode = mode;
  updateComposer();
}

function focusComposer() {
  if (!state.composerOpen) return focusTerminal();
  input.focus();
  setMode('compose');
}

function focusTerminal() {
  state.active?.term.focus();
  setMode('direct');
}

function setComposerOpen(open, { focus = true } = {}) {
  state.composerOpen = open;
  document.body.dataset.composer = open ? 'open' : 'closed';
  btnComposer.setAttribute('aria-pressed', String(open));
  if (!open && state.active) state.active.autoReturn = false;
  if (open) autosize();
  if (!focus) return;
  if (open) focusComposer();
  else focusTerminal();
}

// Ctrl+`：收起时打开；打开但焦点在终端时回到输入框；已在输入框里时收起
function toggleComposer() {
  if (!state.composerOpen) setComposerOpen(true);
  else if (document.activeElement !== input) focusComposer();
  else setComposerOpen(false);
}

// 全屏程序（vim/top/less）运行时键盘直通终端，退出后回到输入框
function syncFocusWithBuffer() {
  const p = state.active;
  if (!p) return;
  if (p.altScreen || !state.composerOpen) focusTerminal();
  else if (document.body.dataset.mode !== 'direct' || !p.autoReturn) focusComposer();
}

function updateComposer() {
  const p = state.active;
  if (!p) return;
  metaTarget.textContent = `${p.target.name}${p.target.detail ? ' · ' + p.target.detail : ''}`;

  let mode = '';
  let warn = false;
  if (p.state === 'closed') mode = t('mode.closed');
  else if (p.state === 'connecting') mode = t('mode.connecting');
  else if (p.state === 'reconnecting') mode = t('mode.reconnecting');
  else if (p.altScreen) mode = t('mode.altScreen');
  else if (p.secret) {
    mode = t('mode.secret');
    warn = true;
  } else if (document.body.dataset.mode === 'direct') mode = t('mode.direct');
  metaMode.textContent = mode;
  metaMode.dataset.kind = warn ? 'warn' : '';

  input.classList.toggle('secret', p.secret);
  input.placeholder = t(p.secret ? 'composer.secretPlaceholder' : 'composer.placeholder');
  document.body.dataset.secret = String(p.secret);
  renderGhost();
}

function autosize() {
  input.style.height = 'auto';
  input.style.height = `${input.scrollHeight}px`;
}

function setComposerValue(v, caret = v.length) {
  input.value = v;
  input.setSelectionRange(caret, caret);
  autosize();
  resetHistoryNav();
  renderGhost();
}

let composing = false;

function ghostVisible() {
  const sh = state.active?.shell;
  const end = input.value.length;
  return !!(sh?.ready && sh.ghost && !composing && input.value === sh.buffer && input.selectionStart === end && input.selectionEnd === end);
}

function renderGhost() {
  const show = ghostVisible();
  ghostTyped.textContent = input.value;
  ghostHint.textContent = show ? state.active.shell.ghost : '';
  ghost.scrollTop = input.scrollTop;
}

// 输入框 → ZLE：整行替换 BUFFER 并设置光标，zsh 随后会把新状态（含灰色提示）报告回来
function shellSync(p) {
  const sh = p.shell;
  if (!p.atPrompt || p.secret) return;
  const v = input.value.replace(/\r\n?/g, '\n');
  const cursor = cpLength(v.slice(0, input.selectionEnd));
  sh.gen++;
  sh.ghost = '';
  p.send(`\x1b[7777~${sh.gen};${cursor};${cpLength(v)};${v}`);
}

function ensureSynced(p) {
  const cursor = cpLength(input.value.slice(0, input.selectionEnd));
  if (input.value !== p.shell.buffer || cursor !== p.shell.cursor) shellSync(p);
}

// ZLE → 输入框：shell 补全、在终端里直接编辑等改动回填到输入框
function applyShellState(p) {
  const sh = p.shell;
  if (!composing && input.value !== sh.buffer) {
    setComposerValue(sh.buffer, cpToIndex(sh.buffer, sh.cursor));
  }
  renderGhost();
}

function payloadFor(pane, text) {
  if (!text.includes('\n')) return text + '\r';
  // 多行内容：shell 开启了 bracketed paste 时整体粘贴再执行，否则逐行执行
  if (pane.term.modes.bracketedPasteMode) return `\x1b[200~${text}\x1b[201~\r`;
  return text.replace(/\r?\n/g, '\r') + '\r';
}

function submit() {
  const p = state.active;
  if (!p) return;
  const text = input.value;
  if (p.state === 'closed' && !text) {
    p.reconnect();
    return;
  }
  if (!p.secret && text.trim()) pushHistory(p.target.id, text);
  resetHistoryNav();
  p.term.scrollToBottom();
  if (p.atPrompt) {
    // 由 ZLE 执行这一行；shell 报告 busy 时再清空输入框
    if (text !== p.shell.buffer) shellSync(p);
    p.send('\r');
    return;
  }
  p.send(payloadFor(p, text));
  setComposerValue('');
}

function acceptGhost() {
  const p = state.active;
  setComposerValue(input.value + p.shell.ghost);
  shellSync(p);
}

/* ---------- 历史 ---------- */

const histKey = (id) => `infiterm:hist:${id}`;

function readHistory(id) {
  try {
    return JSON.parse(localStorage.getItem(histKey(id))) || [];
  } catch {
    return [];
  }
}

function pushHistory(id, text) {
  const list = readHistory(id).filter((h) => h !== text);
  list.push(text);
  localStorage.setItem(histKey(id), JSON.stringify(list.slice(-HISTORY_MAX)));
}

let nav = null;

function resetHistoryNav() {
  nav = null;
}

// 类似 fish：按当前已输入内容作为前缀筛选历史
function navigateHistory(dir) {
  const p = state.active;
  if (!p) return false;
  if (!nav) {
    nav = { list: readHistory(p.target.id), idx: -1, draft: input.value, prefix: input.value.includes('\n') ? '' : input.value };
    nav.idx = nav.list.length;
  }
  let i = nav.idx;
  do {
    i += dir;
  } while (i >= 0 && i < nav.list.length && !nav.list[i].startsWith(nav.prefix));
  if (i < 0) return true;
  const keep = nav;
  keep.idx = Math.min(i, keep.list.length);
  setComposerValue(keep.idx === keep.list.length ? keep.draft : keep.list[keep.idx]);
  nav = keep;
  shellSync(p);
  return true;
}

input.addEventListener('input', () => {
  resetHistoryNav();
  autosize();
  if (!composing && state.active) shellSync(state.active);
  renderGhost();
});

input.addEventListener('compositionstart', () => {
  composing = true;
  renderGhost();
});

input.addEventListener('compositionend', () => {
  composing = false;
  if (state.active) shellSync(state.active);
  renderGhost();
});

input.addEventListener('scroll', () => (ghost.scrollTop = input.scrollTop));
input.addEventListener('focus', () => setMode('compose'));
document.addEventListener('selectionchange', () => {
  if (document.activeElement === input) renderGhost();
});

input.addEventListener('keydown', (e) => {
  const p = state.active;
  const v = input.value;
  const noMods = !e.shiftKey && !e.altKey && !e.metaKey && !e.ctrlKey;

  // Ctrl 组合键先于输入法判断处理：中文输入法下它们可能带着 keyCode 229（Ctrl+C 在全局处理）
  const letter = ctrlLetter(e);
  if ((letter === 'D' || letter === 'Z') && !v) {
    e.preventDefault();
    p?.send(letter === 'D' ? '\x04' : '\x1a');
    return;
  }
  if (letter === 'L') {
    e.preventDefault();
    p?.send('\x0c');
    return;
  }
  if (e.isComposing || e.keyCode === 229) return;

  if (e.key === 'Enter' && noMods) {
    e.preventDefault();
    submit();
  } else if (e.key === 'Tab' && !e.ctrlKey && !e.metaKey && !e.altKey) {
    e.preventDefault();
    if (!p) return;
    if (p.atPrompt) {
      // 交给 shell 自己的补全，结果经 ZLE 回填到输入框
      ensureSynced(p);
      p.send(e.shiftKey ? '\x1b[Z' : '\t');
    } else if (!e.shiftKey) {
      // 没有 shell 集成（如 ssh）：把已输入内容交给 shell，键盘切到终端，回车后再回来
      p.send(v + '\t');
      setComposerValue('');
      p.autoReturn = true;
      focusTerminal();
    }
  } else if ((e.key === 'ArrowRight' || e.key === 'End') && noMods && ghostVisible()) {
    e.preventDefault();
    acceptGhost();
  } else if (e.key === 'ArrowUp' && !e.shiftKey && input.selectionStart === input.selectionEnd) {
    const firstBreak = v.indexOf('\n');
    if (firstBreak < 0 || input.selectionStart <= firstBreak) {
      if (navigateHistory(-1)) e.preventDefault();
    }
  } else if (e.key === 'ArrowDown' && !e.shiftKey && input.selectionStart === input.selectionEnd) {
    if (nav && input.selectionStart > v.lastIndexOf('\n')) {
      if (navigateHistory(1)) e.preventDefault();
    }
  } else if (e.key === 'Escape' && !v) {
    e.preventDefault();
    focusTerminal();
  }
});

/* ---------- 目标选择器 ---------- */

function renderTargetList(container, filter, selectedIndex, onPick) {
  const f = filter.trim().toLowerCase();
  const items = state.targets.filter((tg) => !f || `${tg.id} ${tg.name} ${tg.detail}`.toLowerCase().includes(f));
  container.replaceChildren(
    ...items.map((tg, i) => {
      const a = document.createElement('a');
      a.className = 'target' + (i === selectedIndex ? ' selected' : '');
      a.href = `/?t=${encodeURIComponent(tg.id)}`;
      a.title = t('target.linkTitle');
      const kind = document.createElement('span');
      kind.className = 'kind';
      kind.textContent = KIND_LABEL[tg.kind] || tg.kind;
      const name = document.createElement('span');
      name.className = 'name';
      name.textContent = tg.name;
      const detail = document.createElement('span');
      detail.className = 'detail';
      detail.textContent = tg.detail || '';
      a.append(kind, name, detail);
      a.addEventListener('click', (e) => {
        if (e.metaKey || e.ctrlKey || e.shiftKey) return;
        e.preventDefault();
        onPick(tg);
      });
      return a;
    }),
  );
  return items;
}

let pickerItems = [];
let pickerIndex = 0;

function refreshPicker() {
  pickerItems = renderTargetList(pickerList, pickerFilter.value, pickerIndex, pickFromPicker);
}

function pickFromPicker(t) {
  picker.close();
  openPane(t.id);
}

function togglePicker() {
  if (picker.open) picker.close();
  else openPicker();
}

function openPicker() {
  if (picker.open) return;
  pickerFilter.value = '';
  pickerIndex = 0;
  // Show the current snapshot immediately, then refresh it in the background.
  // This keeps the picker responsive while ensuring edits to ssh/config and
  // targets.json are visible each time it is opened.
  refreshPicker();
  picker.showModal();
  pickerFilter.focus();
  void loadTargets();
}

pickerFilter.addEventListener('input', () => {
  pickerIndex = 0;
  refreshPicker();
});

pickerFilter.addEventListener('keydown', (e) => {
  if (e.isComposing || e.keyCode === 229) return;
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault();
    const n = pickerItems.length;
    if (n) pickerIndex = (pickerIndex + (e.key === 'ArrowDown' ? 1 : -1) + n) % n;
    refreshPicker();
    pickerList.querySelector('.selected')?.scrollIntoView({ block: 'nearest' });
  } else if (e.key === 'Enter') {
    e.preventDefault();
    const t = pickerItems[pickerIndex];
    if (t) pickFromPicker(t);
  }
});

picker.addEventListener('close', () => {
  if (state.active) syncFocusWithBuffer();
});

/* ---------- 输入法下的 Ctrl 组合键 ---------- */

// 中文输入法开启时，Ctrl+字母、Esc 的 keydown 可能报成 keyCode 229，有的输入法干脆吞掉 keydown 只留 keyup；
// xterm 会把 229 当成输入法组合丢弃（xtermjs/xterm.js#6067）。这里按物理键位自行处理，keydown 没收到时用 keyup 兜底
const ctrlDownSeen = new Set();

function keyTarget(e) {
  const p = state.active;
  if (!p) return null;
  if (e.target === input) return 'composer';
  if (e.target === p.term.textarea) return 'terminal';
  return null;
}

function sendCtrl(where, letter) {
  const p = state.active;
  if (letter === 'C') {
    if (where === 'composer') setComposerValue('');
    // 停在 zsh 提示符时像 Warp 一样原地清空当前输入，不留旧行、不另起提示符；命令运行中才发中断
    if (p.atPrompt) {
      clearShellLine(p);
      return;
    }
  }
  p.send(String.fromCharCode(letter.charCodeAt(0) - 64));
}

function clearShellLine(p) {
  p.shell.gen++;
  p.shell.ghost = '';
  p.send(`\x1b[7777~${p.shell.gen};0;0;`);
}

window.addEventListener(
  'keydown',
  (e) => {
    const letter = ctrlLetter(e);
    if (letter) ctrlDownSeen.add(`Key${letter}`);
    const where = keyTarget(e);
    if (!where) return;
    const ime = e.keyCode === 229;
    if (letter === 'C' || (letter && ime && where === 'terminal')) {
      e.preventDefault();
      e.stopPropagation();
      sendCtrl(where, letter);
    } else if (ime && where === 'terminal' && e.code === 'Escape' && !e.isComposing && !e.ctrlKey && !e.metaKey && !e.altKey) {
      e.preventDefault();
      e.stopPropagation();
      state.active.send('\x1b');
    }
  },
  true,
);

window.addEventListener(
  'keyup',
  (e) => {
    const code = keyCodeOf(e);
    if (!/^Key[A-Z]$/.test(code)) return;
    const seen = ctrlDownSeen.delete(code);
    const where = keyTarget(e);
    const letter = ctrlLetter(e);
    if (seen || !where || !letter) return;
    if (where === 'composer' && letter !== 'C') return;
    sendCtrl(where, letter);
  },
  true,
);

window.addEventListener('blur', () => ctrlDownSeen.clear());

// ?debug：右上角显示最近的键盘/输入法事件和发给终端的字节，同时写进服务端日志，排查输入法问题用。
// 可打印文字只记长度，不记内容
const DEBUG = new URLSearchParams(location.search).has('debug');
const debugLines = [];
const debugBox = DEBUG ? document.body.appendChild(Object.assign(document.createElement('pre'), { className: 'key-debug' })) : null;
const hasControl = (s) => /[\x00-\x1f\x7f]/.test(s);
const describeText = (s) => (typeof s !== 'string' ? '-' : hasControl(s) || s.length === 0 ? JSON.stringify(s) : `<${s.length}字>`);

function debugLog(line) {
  if (!DEBUG) return;
  debugLines.push(line);
  debugLines.splice(0, debugLines.length - 16);
  debugBox.textContent = debugLines.join('\n');
  state.active?.sendControl({ t: 'debug', d: line });
}

if (DEBUG) {
  const log = (e) => {
    const f = (v) => (v === undefined ? '-' : JSON.stringify(v));
    const key = typeof e.key === 'string' && e.key.length === 1 && !e.ctrlKey ? '<1字>' : f(e.key);
    const target = e.target === input ? 'composer' : e.target === state.active?.term.textarea ? 'terminal' : e.target?.tagName;
    debugLog(`${e.type.padEnd(16)} @${target} key=${key} code=${f(e.code)} keyCode=${f(e.keyCode)} ctrl=${f(e.ctrlKey)} composing=${f(e.isComposing)} inputType=${f(e.inputType)} data=${describeText(e.data)}`);
  };
  for (const t of ['keydown', 'keyup', 'beforeinput', 'input', 'compositionstart', 'compositionupdate', 'compositionend']) {
    window.addEventListener(t, log, true);
  }
}

/* ---------- 全局快捷键与按钮 ---------- */

function switchBy(delta) {
  const n = state.panes.length;
  if (!n) return;
  const i = state.panes.indexOf(state.active);
  activate(state.panes[(i + delta + n) % n]);
}

// 捕获阶段处理，先于 xterm 拿到按键
window.addEventListener(
  'keydown',
  (e) => {
    // ⌘K 新建终端。Mac 上按住 ⌘ 时字母键收不到 keyup，只能靠 keydown；按物理键位判断，不受输入法影响
    if (e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey && keyCodeOf(e) === 'KeyK') {
      e.preventDefault();
      e.stopPropagation();
      togglePicker();
      return;
    }
    if (!e.ctrlKey || e.metaKey || e.altKey) return;
    let handled = true;
    if (e.shiftKey && e.code === 'KeyT') openPicker();
    else if (e.shiftKey && e.code === 'KeyW') state.active && closePane(state.active);
    else if (e.shiftKey && e.code === 'BracketLeft') switchBy(-1);
    else if (e.shiftKey && e.code === 'BracketRight') switchBy(1);
    else if (!e.shiftKey && e.code === 'Comma') openSettings();
    else if (!e.shiftKey && /^Digit[1-9]$/.test(e.code)) {
      const p = state.panes[Number(e.code.slice(5)) - 1];
      if (p) activate(p);
    } else if (!e.shiftKey && e.code === 'Backquote') {
      toggleComposer();
    } else handled = false;
    if (handled) {
      e.preventDefault();
      e.stopPropagation();
    }
  },
  true,
);

$('#btn-new').addEventListener('click', openPicker);
$('#btn-settings').addEventListener('click', openSettings);
btnComposer.addEventListener('click', () => setComposerOpen(!state.composerOpen));

function recoverPanes() {
  updateKeepAliveLock();
  for (const p of state.panes) p.probeConnection();
  state.active?.fit();
}

document.addEventListener('visibilitychange', () => {
  if (!document.hidden) recoverPanes();
});
document.addEventListener('resume', recoverPanes);
window.addEventListener('online', recoverPanes);
window.addEventListener('pagehide', releaseKeepAliveLock);
window.addEventListener('pageshow', (e) => {
  if (e.persisted) recoverPanes();
});

window.addEventListener('beforeunload', (e) => {
  if (state.panes.some((p) => p.state === 'open' || p.state === 'reconnecting')) e.preventDefault();
});

/* ---------- 启动 ---------- */

async function loadTargets() {
  try {
    const res = await fetch('/api/targets');
    if (!res.ok) throw new Error(`failed to load targets: ${res.status}`);
    state.targets = await res.json();
  } catch {
    state.targets = [];
  }
  localizeTargets();
}

async function init() {
  applySettings();
  setComposerOpen(settings.showComposer, { focus: false });
  await loadTargets();

  const saved = loadTabSessions();
  const params = new URLSearchParams(location.search);
  const ids = params.getAll('t').flatMap((s) => s.split(',')).map((s) => s.trim()).filter(Boolean);
  ids.forEach((id, i) => {
    const sessionId = saved[i]?.target === id ? saved[i].session : null;
    const remoteHost = sessionId && saved[i]?.target === id ? saved[i].remoteHost || '' : '';
    openPane(id, { activateIt: i === 0, sessionId, remoteHost });
  });
  renderTabs();
  if (state.panes[0]) activate(state.panes[0]);
}

init();
