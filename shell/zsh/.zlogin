ZDOTDIR=$PORTHOLE_USER_ZDOTDIR
[[ -r $ZDOTDIR/.zlogin ]] && source $ZDOTDIR/.zlogin
# 启动完成，恢复用户原来的 ZDOTDIR，之后启动的子 shell 不再经过 porthole
if [[ $ZDOTDIR == $HOME ]]; then unset ZDOTDIR; fi
unset PORTHOLE_USER_ZDOTDIR
