# InfiTerm 的 zsh 集成：网页输入框与 ZLE 编辑缓冲区双向同步
#   shell → 网页：OSC 7777;ready | busy | buf;<gen>;<cursor>;<buffer>;<postdisplay>
#   网页 → shell：ESC[7777~<gen>;<cursor>;<字符数>;<内容>，由 infiterm-set 读取后写入 BUFFER
# POSTDISPLAY 就是 zsh-autosuggestions 的灰色提示；补全由 shell 自己完成，结果通过 BUFFER 回传
[[ -o interactive && -o zle ]] || return 0
(( ${+_infiterm_gen} )) && return 0

typeset -gi _infiterm_gen=0
typeset -g _infiterm_last=
typeset -g _infiterm_special='[[:cntrl:]%;]'

_infiterm_osc() {
  print -rn -- $'\e]7777;'"$1"$'\a' >/dev/tty
}

# OSC 里不能出现控制字符，分隔符 ; 和 % 本身也要转义
_infiterm_enc() {
  local s=$1
  if [[ $s != *$~_infiterm_special* ]]; then
    REPLY=$s
    return
  fi
  local out= c h i
  for (( i = 1; i <= $#s; i++ )); do
    c=$s[i]
    if [[ $c == $~_infiterm_special ]]; then
      printf -v h '%%%02X' "'$c"
      out+=$h
    else
      out+=$c
    fi
  done
  REPLY=$out
}

_infiterm_report() {
  local b p
  _infiterm_enc "$BUFFER"; b=$REPLY
  _infiterm_enc "$POSTDISPLAY"; p=$REPLY
  local msg="buf;$_infiterm_gen;$CURSOR;$b;$p"
  [[ $msg == $_infiterm_last ]] && return
  _infiterm_last=$msg
  _infiterm_osc $msg
}

_infiterm_line_init() {
  _infiterm_last=
  _infiterm_osc ready
}

_infiterm_line_finish() {
  _infiterm_osc busy
}

# 名字不能以 _ 开头，否则 zsh-autosuggestions 不会在它之后刷新提示
infiterm-set() {
  local c gen= cur= n= text=
  while read -k 1 c && [[ $c != ';' ]]; do gen+=$c; done
  while read -k 1 c && [[ $c != ';' ]]; do cur+=$c; done
  while read -k 1 c && [[ $c != ';' ]]; do n+=$c; done
  [[ $gen == <-> && $cur == <-> && $n == <-> ]] || return 1
  (( n > 0 )) && read -k $n text
  _infiterm_gen=$gen
  BUFFER=$text
  CURSOR=$cur
}

zle -N infiterm-set
for _infiterm_km in emacs viins vicmd; do
  bindkey -M $_infiterm_km '\e[7777~' infiterm-set
done
unset _infiterm_km

autoload -Uz add-zle-hook-widget
add-zle-hook-widget line-init _infiterm_line_init
add-zle-hook-widget line-finish _infiterm_line_finish
add-zle-hook-widget line-pre-redraw _infiterm_report

# zsh-autosuggestions 的异步模式在后台算出提示后经 autosuggest-suggest 写入，不触发 line-pre-redraw，需要单独补报
# 备份的原 widget 必须以 _ 开头，否则会被 autosuggestions 再包一层，把刚写入的提示清掉
if (( ${+widgets[autosuggest-suggest]} )); then
  zle -A autosuggest-suggest _infiterm_orig_autosuggest_suggest
  _infiterm_autosuggest_suggest() {
    zle _infiterm_orig_autosuggest_suggest -- "$@"
    _infiterm_report
  }
  zle -N autosuggest-suggest _infiterm_autosuggest_suggest
fi
