# hads — predictive shell for bash  (https://hads.sh)
#   Ctrl+Space   fill the predicted command (works on an empty line too: "what's next?")
#   Alt+N        cycle through alternatives
#   ask <text>   natural-language mode (if enabled), e.g.  ask update the system and install claude code
[[ $- == *i* ]] || return 0
[[ -n ${_HADS_LOADED:-} ]] && return 0
_HADS_LOADED=1
_HADS_BIN=__HADS_BIN__
_HADS_SESS="bash-$$"

__hads_histnum() {
  local line
  line=$(HISTTIMEFORMAT= builtin history 1)
  [[ $line =~ ^[[:space:]]*([0-9]+)[*[:space:]]+(.*)$ ]] || return 1
  __hads_num=${BASH_REMATCH[1]}
  __hads_cmd=${BASH_REMATCH[2]}
}
__hads_prompt() {
  local ec=$?
  # bash loads ~/.bash_history after the rc file: the first prompt only sets the baseline
  if [[ -z ${__hads_ready:-} ]]; then
    __hads_ready=1
    __hads_histnum && __hads_last_num=$__hads_num
    return $ec
  fi
  if __hads_histnum && [[ $__hads_num != "${__hads_last_num:-}" ]]; then
    __hads_last_num=$__hads_num
    "$_HADS_BIN" record --shell bash --sess "$_HADS_SESS" --exit "$ec" --cwd "$PWD" -- "$__hads_cmd" 2>/dev/null
  fi
  return $ec
}
if [[ "$(declare -p PROMPT_COMMAND 2>/dev/null)" == "declare -a"* ]]; then
  PROMPT_COMMAND=(__hads_prompt "${PROMPT_COMMAND[@]}")
else
  PROMPT_COMMAND="__hads_prompt${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
fi

__hads_predict() {
  "$_HADS_BIN" predict --shell bash --sess "$_HADS_SESS" --cwd "$PWD" --limit "$1" -- "$2" 2>/dev/null
}
__hads_fill() {
  local out
  out=$(__hads_predict 1 "$READLINE_LINE")
  [[ -n $out ]] || return 0
  READLINE_LINE=$out
  READLINE_POINT=${#READLINE_LINE}
  __hads_cur=$READLINE_LINE
}
__hads_cycle() {
  if [[ "$READLINE_LINE" != "${__hads_cur-__none__}" ]]; then
    __hads_base=$READLINE_LINE
    mapfile -t __hads_list < <(__hads_predict 6 "$__hads_base")
    __hads_i=-1
  fi
  (( ${#__hads_list[@]} )) || return 0
  __hads_i=$(( (__hads_i + 1) % ${#__hads_list[@]} ))
  READLINE_LINE=${__hads_list[$__hads_i]}
  READLINE_POINT=${#READLINE_LINE}
  __hads_cur=$READLINE_LINE
}
if (( BASH_VERSINFO[0] >= 4 )); then
  bind -x '"\C-@": __hads_fill' 2>/dev/null   # Ctrl+Space
  bind -x '"\en": __hads_cycle' 2>/dev/null   # Alt+N
fi

# a plain-language request typed as a command (Arabic, or an English sentence) goes to smart mode
if declare -F command_not_found_handle >/dev/null && ! declare -F __hads_orig_cnf >/dev/null; then
  eval "$(declare -f command_not_found_handle | sed '1s/command_not_found_handle/__hads_orig_cnf/')"
fi
command_not_found_handle() {
  if "$_HADS_BIN" cnf -- "$@" 2>/dev/null; then
    "$_HADS_BIN" ask "$*"
    return $?
  fi
  if declare -F __hads_orig_cnf >/dev/null; then
    __hads_orig_cnf "$@"
    return $?
  fi
  printf 'bash: %s: command not found\n' "$1" >&2
  return 127
}

if ! type -t ask >/dev/null 2>&1; then
  ask() { "$_HADS_BIN" ask "$*"; }
fi
