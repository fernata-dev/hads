# hads — predictive shell for fish  (https://hads.sh)
#   The predicted next command shows on the right of your prompt.
#   Ctrl+Space   fill the prediction        Alt+N   cycle alternatives
#   ? <request>  natural-language mode (if enabled)
status is-interactive; or exit 0
set -q __hads_loaded; and exit 0
set -g __hads_loaded 1
set -g __hads_bin __HADS_BIN__
set -g __hads_sess "fish-$fish_pid"
set -g __hads_next
set -g __hads_list
set -g __hads_i 0
set -g __hads_cur __none__

function __hads_postexec --on-event fish_postexec
    set -l ec $status
    test -n "$argv[1]"; or return
    $__hads_bin record --shell fish --sess $__hads_sess --exit $ec --cwd "$PWD" -- "$argv[1]" 2>/dev/null
    set -g __hads_next ($__hads_bin predict --shell fish --sess $__hads_sess --cwd "$PWD" --limit 1 -- "" 2>/dev/null)
end

function __hads_on_pwd --on-variable PWD
    set -g __hads_next ($__hads_bin predict --shell fish --sess $__hads_sess --cwd "$PWD" --limit 1 -- "" 2>/dev/null)
end

if functions -q fish_right_prompt; and not functions -q __hads_orig_right_prompt
    functions -c fish_right_prompt __hads_orig_right_prompt
end
function fish_right_prompt
    set -l cl (commandline)
    if not set -q hads_no_hint; and test -n "$__hads_next[1]"; and test -z "$cl"
        set_color brblack
        echo -n "⟡ $__hads_next[1]  ⌃␣ "
        set_color normal
    end
    functions -q __hads_orig_right_prompt; and __hads_orig_right_prompt
end

function __hads_fill
    set -l buf (string join \n -- (commandline))
    set -l s ($__hads_bin predict --shell fish --sess $__hads_sess --cwd "$PWD" --limit 1 -- "$buf" 2>/dev/null)
    if test -n "$s[1]"
        commandline -r -- $s[1]
        commandline -f end-of-line
        set -g __hads_cur $s[1]
    end
end

function __hads_cycle
    set -l cur (string join \n -- (commandline))
    if test "$cur" != "$__hads_cur"
        set -g __hads_list ($__hads_bin predict --shell fish --sess $__hads_sess --cwd "$PWD" --limit 6 -- "$cur" 2>/dev/null)
        set -g __hads_i 0
    end
    set -l n (count $__hads_list)
    test $n -gt 0; or return
    set -g __hads_i (math "$__hads_i % $n + 1")
    set -g __hads_cur $__hads_list[$__hads_i]
    commandline -r -- $__hads_cur
    commandline -f end-of-line
end

function __hads_enter
    set -l line (string join \n -- (commandline))
    set -l bin hads
    command -q hads; or set bin (string escape -- $__hads_bin)
    if string match -qr '^(\?|؟)\s+\S' -- $line
        set -l req (string replace -r '^(\?|؟)\s+' '' -- $line)
        commandline -r -- "$bin ask "(string escape -- $req)
    else if test -n "$line"
        # a plain-language request typed as a command (Arabic, or an English sentence)
        set -l words (string split -n ' ' -- $line)
        if not string match -q '*/*' -- $words[1]; and not type -q -- $words[1]; and $__hads_bin cnf -- $words 2>/dev/null
            commandline -r -- "$bin ask "(string escape -- $line)
        end
    end
    commandline -f execute
end

if not functions -q ask; and not command -q ask
    function ask --description 'hads natural-language mode'
        $__hads_bin ask (string join ' ' -- $argv)
    end
end

if string match -qr '^[4-9]' -- $version
    for m in default insert
        bind -M $m ctrl-space __hads_fill
        bind -M $m alt-n __hads_cycle
        bind -M $m enter __hads_enter
    end
else
    for m in default insert
        bind -M $m -k nul __hads_fill 2>/dev/null
        bind -M $m \en __hads_cycle
        bind -M $m \r __hads_enter
    end
end
set -g __hads_next ($__hads_bin predict --shell fish --sess $__hads_sess --cwd "$PWD" --limit 1 -- "" 2>/dev/null)
