#!/usr/bin/env python3
"""Generates docs/demo.svg and docs/banner.svg (pure CSS animation, renders on GitHub).
Run:  python3 docs/gen_svg.py
"""
from pathlib import Path
from xml.sax.saxutils import escape

OUT = Path(__file__).parent
W, H = 860, 470
TOTAL = 16.0  # seconds per loop
CH = 9.03     # monospace advance at 15px
BG = "#0b1018"

# (typed, ghost, reason, accepted_text)
SCENES = [
    ("git com", 'mit -m "fix: login redirect"', "after git add", None),
    ("", "git push", "1 commit not pushed", None),
    ("cp rep", "  ⟶ cp Documents/report-2026.pdf ~/Desktop/", "your usual spot", "cp Documents/report-2026.pdf ~/Desktop/"),
    ("cd web", "  ⟶ cd ~/code/webapp", "frequent folder", "cd ~/code/webapp"),
    ("gti pu", "  ⟶ git push", "typo fixed", "git push"),
]

css, body = [], []
n = 0


def pct(t):
    return f"{max(0.0, min(100.0, t / TOTAL * 100)):.3f}%"


def show(start, end=TOTAL - 0.6):
    """opacity 0 -> 1 at start, back to 0 at end."""
    global n
    n += 1
    name = f"s{n}"
    css.append(
        f"@keyframes {name}{{0%,{pct(start - 0.001)}{{opacity:0}}{pct(start)},{pct(end)}{{opacity:1}}{pct(end + 0.3)},100%{{opacity:0}}}}"
        f".{name}{{opacity:0;animation:{name} {TOTAL}s infinite}}"
    )
    return name


def typing(x, y, chars, start, dur):
    """a cover rect that slides right in steps to reveal typed text."""
    global n
    n += 1
    name = f"t{n}"
    w = chars * CH + 4
    css.append(
        f"@keyframes {name}{{0%,{pct(start)}{{transform:translateX(0)}}{pct(start + dur)},100%{{transform:translateX({w:.1f}px)}}}}"
        f".{name}{{animation:{name} {TOTAL}s steps({max(chars,1)},end) infinite}}"
    )
    return f'<rect class="{name}" x="{x-2}" y="{y-16}" width="{w+2:.1f}" height="22" fill="{BG}"/>'


y0, lh = 112, 34
t = 0.6
prompt_x, text_x = 40, 62
for i, (typed, ghost, reason, accepted) in enumerate(SCENES):
    y = y0 + i * lh
    line_start = t
    body.append(f'<g class="{show(line_start)}"><text x="{prompt_x}" y="{y}" class="p">❯</text>')
    if typed:
        body.append(f'<text x="{text_x}" y="{y}" class="ink">{escape(typed)}</text>')
        body.append(typing(text_x, y, len(typed), line_start + 0.1, 0.07 * len(typed) + 0.1))
        t += 0.1 + 0.07 * len(typed) + 0.35
    else:
        t += 0.4
    body.append("</g>")
    gx = text_x + len(typed) * CH
    ghost_on, accept_at = t, t + 1.5
    # ghost text + reason chip, visible until accepted
    rx = gx + len(ghost) * CH + 14
    body.append(
        f'<g class="{show(ghost_on, accept_at - 0.31)}"><text x="{gx:.1f}" y="{y}" class="ghost">{escape(ghost)}</text>'
        f'<rect x="{rx:.1f}" y="{y-15}" rx="6" width="{len(reason)*7.3+16:.1f}" height="21" class="chip"/>'
        f'<text x="{rx+8:.1f}" y="{y}" class="why">{escape(reason)}</text></g>'
    )
    # accepted state: the full command in ink + tick
    final = accepted if accepted is not None else typed + ghost
    body.append(
        f'<g class="{show(accept_at)}"><rect x="{text_x-2}" y="{y-17}" width="{W-text_x-20}" height="24" fill="{BG}"/>'
        f'<text x="{text_x}" y="{y}" class="ink">{escape(final)}</text>'
        f'<text x="{text_x + len(final)*CH + 12:.1f}" y="{y}" class="ok">✔</text></g>'
    )
    t = accept_at + 0.5

# smart mode line at the bottom
ys = y0 + len(SCENES) * lh + 22
body.append(f'<g class="{show(t)}"><text x="{prompt_x}" y="{ys}" class="p">❯</text>'
            f'<text x="{text_x}" y="{ys}" class="ink">? update the system and install claude code</text>'
            + typing(text_x, ys, 44, t + 0.1, 2.0) + "</g>")
t2 = t + 2.6
plan = [("1", "Full system upgrade", "sudo pacman -Syu --noconfirm", "medium"),
        ("2", "Install Claude Code", "curl -fsSL https://claude.ai/install.sh | bash", "medium"),
        ("3", "Verify", "claude --version", "low")]
for j, (num, title, cmd, risk) in enumerate(plan):
    yy = ys + 30 + j * 24
    col = "#ffb86b" if risk == "medium" else "#7ee2a8"
    body.append(f'<g class="{show(t2 + j*0.35)}"><text x="{text_x}" y="{yy}" class="num">{num}</text>'
                f'<text x="{text_x+22}" y="{yy}" class="dim">{escape(title)}</text>'
                f'<text x="{text_x+210}" y="{yy}" class="cmd">$ {escape(cmd)}</text>'
                f'<text x="{W-40}" y="{yy}" text-anchor="end" fill="{col}" class="sm">{risk}</text></g>')

demo = f'''<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" role="img" aria-label="hads predicting whole shell commands in a terminal">
<defs>
  <linearGradient id="edge" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#4fe3c1" stop-opacity=".7"/><stop offset=".45" stop-color="#ffffff" stop-opacity=".06"/><stop offset="1" stop-color="#3aa0ff" stop-opacity=".6"/></linearGradient>
  <linearGradient id="glass" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#121a26"/><stop offset="1" stop-color="{BG}"/></linearGradient>
</defs>
<style>
text{{font-family:"JetBrains Mono","SFMono-Regular",Menlo,Consolas,monospace;font-size:15px}}
.p{{fill:#4fe3c1}}.ink{{fill:#e9eef6}}.ghost{{fill:#5d6a7d}}.ok{{fill:#7ee2a8}}.why{{fill:#4fe3c1;font-size:12px}}.chip{{fill:#4fe3c1;fill-opacity:.1}}
.dim{{fill:#aab5c5;font-size:13px}}.cmd{{fill:#c9d3e0;font-size:13px}}.num{{fill:#ffb86b;font-size:13px}}.sm{{font-size:12px}}.bar{{fill:#8d99ab;font-size:12px}}
{"".join(css)}
@media (prefers-reduced-motion:reduce){{*{{animation:none!important;opacity:1!important}}}}
</style>
<rect x="1" y="1" width="{W-2}" height="{H-2}" rx="18" fill="{BG}" stroke="url(#edge)" stroke-width="1.5"/>
<circle cx="30" cy="30" r="6" fill="#ff5f57"/><circle cx="50" cy="30" r="6" fill="#febc2e"/><circle cx="70" cy="30" r="6" fill="#28c840"/>
<text x="{W-24}" y="34" text-anchor="end" class="bar">~/projects/webapp — zsh + hads</text>
<line x1="1" y1="56" x2="{W-1}" y2="56" stroke="#ffffff" stroke-opacity=".08"/>
<text x="40" y="84" class="bar">ghost text = hads' prediction · → accepts it</text>
{"".join(body)}
</svg>
'''
(OUT / "demo.svg").write_text(demo, encoding="utf-8")

banner = f'''<svg xmlns="http://www.w3.org/2000/svg" width="1280" height="320" viewBox="0 0 1280 320" role="img" aria-label="hads — the terminal that knows what you'll type next">
<defs>
  <radialGradient id="a" cx=".18" cy=".1" r=".7"><stop offset="0" stop-color="#0f6f8f" stop-opacity=".55"/><stop offset="1" stop-color="#06090f" stop-opacity="0"/></radialGradient>
  <radialGradient id="b" cx=".9" cy=".9" r=".6"><stop offset="0" stop-color="#118a6c" stop-opacity=".45"/><stop offset="1" stop-color="#06090f" stop-opacity="0"/></radialGradient>
  <linearGradient id="m" x1="0" x2="1"><stop offset="0" stop-color="#4fe3c1"/><stop offset="1" stop-color="#3aa0ff"/></linearGradient>
</defs>
<style>
.w{{font-family:"Bricolage Grotesque","Inter Display",system-ui,sans-serif;font-weight:500;letter-spacing:-4px}}
.t{{font-family:"JetBrains Mono",Menlo,monospace}}
.cur{{animation:c 1.1s steps(1) infinite}}@keyframes c{{50%{{opacity:0}}}}
.g{{animation:g 6s ease-in-out infinite}}@keyframes g{{0%,15%{{opacity:0}}30%,80%{{opacity:1}}95%,100%{{opacity:0}}}}
</style>
<rect width="1280" height="320" rx="24" fill="#06090f"/>
<rect width="1280" height="320" rx="24" fill="url(#a)"/><rect width="1280" height="320" rx="24" fill="url(#b)"/>
<rect x="72" y="92" width="64" height="64" rx="18" fill="url(#m)"/>
<text x="104" y="136" text-anchor="middle" class="t" font-size="30" fill="#04121a">❯_</text>
<text x="160" y="152" class="w" font-size="84" fill="#e9eef6">hads</text>
<text x="390" y="150" font-size="54" fill="#8d99ab" font-family="'IBM Plex Sans Arabic','Noto Sans Arabic',sans-serif">حدس</text>
<text x="74" y="214" class="t" font-size="22" fill="#e9eef6">❯ git com<tspan class="g" fill="#5d6a7d">mit -m "ship it"</tspan><tspan class="cur" fill="#4fe3c1">▍</tspan></text>
<text x="74" y="262" font-size="20" fill="#8d99ab" font-family="system-ui,sans-serif">The terminal that knows what you'll type next — on-device, every OS.</text>
</svg>
'''
(OUT / "banner.svg").write_text(banner, encoding="utf-8")
print("wrote demo.svg and banner.svg")
