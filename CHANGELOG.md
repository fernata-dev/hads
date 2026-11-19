# Changelog

## v1.1.0

- **Type requests directly.** A line that starts in Arabic, or an English sentence that isn't a
  command, goes to smart mode in zsh, fish, bash and PowerShell. No `?` needed. Existing
  "command not found" handlers (pkgfile, Ubuntu) keep working.
- **Free models just work.** Automatic fallback to free OpenRouter models when the chosen model fails
  (unusable output, rate limit, no credits, model removed).
- **Robust plan parsing:** loose JSON, renamed fields, `<think>` blocks, code blocks and `$ cmd`
  lines; copied format templates are rejected; empty replies from reasoning models are recovered.
- Missing `sudo` is added for system package managers (never for AUR helpers or Homebrew); Arch
  plans avoid partial upgrades (`pacman -Sy`).
- Open-ended requests ("install a fun game") get a sensible, well-known pick instead of a refusal.
- `HADS_DEBUG=1` prints raw model replies.
- New site: WebGL liquid background, smooth scrolling, a pinned scroll scene of how a prediction
  happens, horizontal feature gallery, Arabic RTL fixes.

## v1.0.0

First release.

- On-device predictor: n-gram transitions, frecency, folder context, learned copy destinations,
  typo repair, git and project rules.
- Background file index with instant `hads find`, `cd` jumps and full `cp`/`mv` predictions.
- Shell integrations: zsh (async ghost text, zsh-autosuggestions strategy), bash, fish, PowerShell.
- History import from zsh, bash, fish and PowerShell; secret filtering.
- Smart mode via OpenRouter: Arabic and English requests, per-machine plans, approval, local safety
  guard, automatic repair of failed steps, file lookups.
- Autostart on systemd, launchd and Windows; one-command installers with SHA-256 verification.
- Builds for macOS, Linux, Windows and FreeBSD on x86-64 and ARM.
