# porthole 的 zsh 集成：网页输入框与 ZLE 编辑缓冲区双向同步
#   shell → 网页：OSC 7777;ready | busy | buf;<gen>;<cursor>;<buffer>;<postdisplay>
#   网页 → shell：ESC[7777~<gen>;<cursor>;<字符数>;<内容>，由 porthole-set 读取后写入 BUFFER
# POSTDISPLAY 就是 zsh-autosuggestions 的灰色提示；补全由 shell 自己完成，结果通过 BUFFER 回传
[[ -o interactive && -o zle ]] || return 0
(( ${+_porthole_gen} )) && return 0

typeset -gi _porthole_gen=0
typeset -g _porthole_last=
typeset -g _porthole_special='[[:cntrl:]%;]'

_porthole_osc() {
  print -rn -- $'\e]7777;'"$1"$'\a' >/dev/tty
}

# OSC 里不能出现控制字符，分隔符 ; 和 % 本身也要转义
_porthole_enc() {
  local s=$1
  if [[ $s != *$~_porthole_special* ]]; then
    REPLY=$s
    return
  fi
  local out= c h i
  for (( i = 1; i <= $#s; i++ )); do
    c=$s[i]
    if [[ $c == $~_porthole_special ]]; then
      printf -v h '%%%02X' "'$c"
      out+=$h
    else
      out+=$c
    fi
  done
  REPLY=$out
}

_porthole_report() {
  local b p
  _porthole_enc "$BUFFER"; b=$REPLY
  _porthole_enc "$POSTDISPLAY"; p=$REPLY
  local msg="buf;$_porthole_gen;$CURSOR;$b;$p"
  [[ $msg == $_porthole_last ]] && return
  _porthole_last=$msg
  _porthole_osc $msg
}

_porthole_line_init() {
  _porthole_last=
  _porthole_osc ready
}

_porthole_line_finish() {
  _porthole_osc busy
}

# 名字不能以 _ 开头，否则 zsh-autosuggestions 不会在它之后刷新提示
porthole-set() {
  local c gen= cur= n= text=
  while read -k 1 c && [[ $c != ';' ]]; do gen+=$c; done
  while read -k 1 c && [[ $c != ';' ]]; do cur+=$c; done
  while read -k 1 c && [[ $c != ';' ]]; do n+=$c; done
  [[ $gen == <-> && $cur == <-> && $n == <-> ]] || return 1
  (( n > 0 )) && read -k $n text
  _porthole_gen=$gen
  BUFFER=$text
  CURSOR=$cur
}

zle -N porthole-set
for _porthole_km in emacs viins vicmd; do
  bindkey -M $_porthole_km '\e[7777~' porthole-set
done
unset _porthole_km

autoload -Uz add-zle-hook-widget
add-zle-hook-widget line-init _porthole_line_init
add-zle-hook-widget line-finish _porthole_line_finish
add-zle-hook-widget line-pre-redraw _porthole_report

# zsh-autosuggestions 的异步模式在后台算出提示后经 autosuggest-suggest 写入，不触发 line-pre-redraw，需要单独补报
# 备份的原 widget 必须以 _ 开头，否则会被 autosuggestions 再包一层，把刚写入的提示清掉
if (( ${+widgets[autosuggest-suggest]} )); then
  zle -A autosuggest-suggest _porthole_orig_autosuggest_suggest
  _porthole_autosuggest_suggest() {
    zle _porthole_orig_autosuggest_suggest -- "$@"
    _porthole_report
  }
  zle -N autosuggest-suggest _porthole_autosuggest_suggest
fi
