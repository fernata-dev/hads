# hads — predictive shell for zsh  (https://hads.sh)
# Ghost text predicts the whole command before you type it.
#   →  / End        accept the prediction
#   Ctrl+Space      fill the prediction now
#   Alt+N / Alt+P   cycle through alternatives
#   ? <request>     natural-language mode (if enabled), e.g.  ? update the system and install claude code
[[ -o interactive ]] || return 0
(( ${+_HADS_LOADED} )) && return 0
typeset -g _HADS_LOADED=1
typeset -g _HADS_BIN=__HADS_BIN__
typeset -g _HADS_SESS="zsh-$$"
typeset -g _HADS_LAST="" _HADS_FD="" _HADS_PID="" _HADS_HL="" _HADS_REQ=""
typeset -ga _HADS_SUGG
typeset -gi _HADS_IDX=1
typeset -g HADS_GHOST_STYLE=${HADS_GHOST_STYLE:-fg=8}
zmodload zsh/system 2>/dev/null
autoload -Uz add-zsh-hook

# ---------- learning: record every command with its folder and exit code ----------
_hads_preexec() { _HADS_LAST=$1 }
_hads_precmd() {
  local ec=$?
  if [[ -n $_HADS_LAST ]]; then
    "$_HADS_BIN" record --shell zsh --sess $_HADS_SESS --exit $ec --cwd "$PWD" -- "$_HADS_LAST" 2>/dev/null
    _HADS_LAST=""
  fi
}
add-zsh-hook preexec _hads_preexec
precmd_functions=(_hads_precmd ${precmd_functions:#_hads_precmd})

# ---------- shortcut: `ask <anything>` ----------
(( $+commands[ask] || $+aliases[ask] || $+functions[ask] )) || alias ask="noglob ${(q)_HADS_BIN} ask"

_hads_name() { (( $+commands[hads] )) && print -r -- hads || print -r -- ${(q)_HADS_BIN} }

# ---------- zsh-autosuggestions present? plug into it as a strategy ----------
if (( $+functions[_zsh_autosuggest_start] )) || (( ${+ZSH_AUTOSUGGEST_STRATEGY} )); then
  _zsh_autosuggest_strategy_hads() {
    typeset -g suggestion
    local out
    out=$("$_HADS_BIN" predict --shell zsh --sess $_HADS_SESS --cwd "$PWD" --limit 3 --prefix-only -- "$1" 2>/dev/null)
    suggestion=${${(f)out}[1]}
  }
  ZSH_AUTOSUGGEST_STRATEGY=(hads ${ZSH_AUTOSUGGEST_STRATEGY:#hads})
  typeset -g _HADS_EXTERNAL_GHOST=1
fi

# ---------- ghost text ----------
_hads_hl_clear() {
  if [[ -n $_HADS_HL ]]; then
    region_highlight=( "${(@)region_highlight:#$_HADS_HL}" )
    _HADS_HL=""
  fi
}
_hads_clear() {
  _hads_hl_clear
  (( ${+_HADS_EXTERNAL_GHOST} )) || POSTDISPLAY=""
}
_hads_render() {
  (( ${+_HADS_EXTERNAL_GHOST} )) && return
  _hads_hl_clear
  local s=${_HADS_SUGG[$_HADS_IDX]}
  if [[ -z $s || $s == "$BUFFER" || $CURSOR -ne $#BUFFER ]]; then POSTDISPLAY=""; return; fi
  if [[ $s == "$BUFFER"* ]]; then
    POSTDISPLAY=${s#"$BUFFER"}
  else
    POSTDISPLAY="  ⟶ $s"
  fi
  (( _HADS_IDX > 1 )) && POSTDISPLAY+="  ‹$_HADS_IDX/${#_HADS_SUGG}›"
  _HADS_HL="$#BUFFER $(( $#BUFFER + $#POSTDISPLAY )) $HADS_GHOST_STYLE"
  region_highlight+=( "$_HADS_HL" )
}

# ---------- async prediction (never blocks typing) ----------
_hads_cancel() {
  if [[ -n $_HADS_FD ]] && { true <&$_HADS_FD } 2>/dev/null; then
    zle -F $_HADS_FD 2>/dev/null
    exec {_HADS_FD}<&-
  fi
  [[ -n $_HADS_PID ]] && kill -TERM $_HADS_PID 2>/dev/null
  _HADS_FD="" _HADS_PID=""
}
_hads_fetch() {
  _hads_cancel
  _HADS_REQ=$1
  exec {_HADS_FD}< <(
    print -r -- $sysparams[pid]
    "$_HADS_BIN" predict --shell zsh --sess $_HADS_SESS --cwd "$PWD" --limit 5 -- "$1" 2>/dev/null
  )
  read -r _HADS_PID <&$_HADS_FD
  zle -F $_HADS_FD _hads_on_result
}
_hads_on_result() {
  emulate -L zsh
  local fd=$1 data=""
  if [[ -z $2 || $2 == hup ]]; then
    IFS='' read -rd '' -u $fd data
    zle _hads_apply -- "$data"
    zle -R
  fi
  zle -F $fd 2>/dev/null
  exec {fd}<&-
  _HADS_FD="" _HADS_PID=""
}
_hads_apply() {
  [[ $BUFFER == "$_HADS_REQ" ]] || return 0
  _HADS_SUGG=( ${(f)1} )
  _HADS_IDX=1
  _hads_render
}
zle -N _hads_apply

_hads_after_edit() {
  if (( PENDING > 0 || KEYS_QUEUED_COUNT > 0 )) || (( CURSOR != $#BUFFER )); then
    _hads_clear; return
  fi
  # keep the ghost steady while typing into it, then refresh
  _HADS_SUGG=( ${(M)_HADS_SUGG:#${~${(b)BUFFER}}*} )
  _HADS_IDX=1
  _hads_render
  _hads_fetch "$BUFFER"
}

_hads_accept() {
  local s=${_HADS_SUGG[$_HADS_IDX]}
  [[ -n $s && $s != "$BUFFER" ]] || return 1
  BUFFER=$s
  CURSOR=$#BUFFER
  [[ $BUFFER == *'""' ]] && CURSOR=$(( $#BUFFER - 1 ))   # git commit -m "|"
  _HADS_SUGG=() _HADS_IDX=1
  _hads_clear
  return 0
}

# ---------- widgets ----------
_hads_w_edit()  { zle _hads_orig_$WIDGET -- "$@"; local r=$?; _hads_after_edit; return r }
_hads_w_reset() { _hads_cancel; _HADS_SUGG=(); _hads_clear; zle _hads_orig_$WIDGET -- "$@" }
_hads_w_fwd() {
  if (( ! ${+_HADS_EXTERNAL_GHOST} )) && (( CURSOR == $#BUFFER )) && [[ -n $POSTDISPLAY ]] && _hads_accept; then
    _hads_fetch "$BUFFER"; return 0
  fi
  zle _hads_orig_$WIDGET -- "$@"
}
_hads_w_accept_line() {
  _hads_cancel; _HADS_SUGG=(); _hads_clear
  if [[ $BUFFER == ('? '|'؟ ')* ]]; then
    local req=${BUFFER[3,-1]}
    BUFFER="$(_hads_name) ask ${(q-)req}"
  fi
  zle _hads_orig_$WIDGET -- "$@"
}
_hads_w_line_init() {
  (( $+widgets[_hads_orig_zle-line-init] )) && zle _hads_orig_zle-line-init -- "$@"
  _HADS_SUGG=()
  [[ -z $BUFFER ]] && (( ! ${+_HADS_EXTERNAL_GHOST} )) && _hads_fetch ""
}
_hads_fill() {
  if (( ${#_HADS_SUGG} == 0 )); then
    _HADS_SUGG=( ${(f)"$("$_HADS_BIN" predict --shell zsh --sess $_HADS_SESS --cwd "$PWD" --limit 5 -- "$BUFFER" 2>/dev/null)"} )
    _HADS_IDX=1
  fi
  _hads_accept && _hads_fetch "$BUFFER"
}
_hads_cycle() {
  local dir=$1
  if (( ${#_HADS_SUGG} == 0 )); then
    _HADS_SUGG=( ${(f)"$("$_HADS_BIN" predict --shell zsh --sess $_HADS_SESS --cwd "$PWD" --limit 6 -- "$BUFFER" 2>/dev/null)"} )
    _HADS_IDX=0
  fi
  (( ${#_HADS_SUGG} )) || return
  _HADS_IDX=$(( (_HADS_IDX - 1 + dir + ${#_HADS_SUGG}) % ${#_HADS_SUGG} + 1 ))
  if (( ${+_HADS_EXTERNAL_GHOST} )); then
    BUFFER=${_HADS_SUGG[$_HADS_IDX]}; CURSOR=$#BUFFER
  else
    _hads_render
  fi
}
_hads_next() { _hads_cycle 1 }
_hads_prev() { _hads_cycle -1 }
zle -N _hads_fill
zle -N _hads_next
zle -N _hads_prev

_hads_bind() {
  local kind=$1 w; shift
  for w in "$@"; do
    (( $+widgets[$w] )) || continue
    [[ $widgets[$w] == user:_hads_w_* ]] && continue
    zle -A $w _hads_orig_$w
    zle -N $w _hads_w_$kind
  done
}
if (( ! ${+_HADS_EXTERNAL_GHOST} )); then
  _hads_bind edit self-insert backward-delete-char delete-char delete-char-or-list backward-kill-word \
    kill-word kill-line backward-kill-line kill-whole-line yank bracketed-paste magic-space \
    vi-backward-delete-char vi-delete-char vi-backward-kill-word
  _hads_bind fwd forward-char end-of-line vi-forward-char vi-end-of-line vi-add-eol
  _hads_bind reset up-line-or-history down-line-or-history up-line-or-search down-line-or-search \
    up-line-or-beginning-search down-line-or-beginning-search history-search-backward \
    history-search-forward history-incremental-search-backward history-incremental-search-forward \
    history-beginning-search-backward history-beginning-search-forward beginning-of-line \
    backward-char vi-backward-char vi-beginning-of-line send-break expand-or-complete complete-word
  (( $+widgets[zle-line-init] )) && zle -A zle-line-init _hads_orig_zle-line-init
  zle -N zle-line-init _hads_w_line_init
fi
_hads_bind accept_line accept-line

for _hads_km in emacs viins; do
  bindkey -M $_hads_km '^@' _hads_fill      # Ctrl+Space
  bindkey -M $_hads_km '^[n' _hads_next     # Alt+N
  bindkey -M $_hads_km '^[p' _hads_prev     # Alt+P
done
unset _hads_km
