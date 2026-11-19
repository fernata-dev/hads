# Security

## Reporting

Please report vulnerabilities privately through
[GitHub security advisories](https://github.com/fernata-dev/hads/security/advisories/new), not in public
issues. You'll get a reply within a few days.

## What hads does to protect you

- **Local only by default.** Prediction, history and the file index stay on your machine. The daemon
  listens on a user-only (`0600`) unix socket; on Windows on loopback with a random per-run token stored
  in your profile.
- **Secrets are never stored.** Commands that look like they contain tokens, passwords, API keys,
  `Authorization` headers or `user:pass@` URLs are dropped before they reach the model or the disk.
- **Verified installs.** The installers check the SHA-256 of every binary against the release's
  `checksums.txt` and refuse to install on a mismatch.
- **Smart mode is opt-in** and never runs a command without your approval. A local guard, independent
  of the model, blocks destructive commands and requires typing `yes` for risky ones. The OpenRouter
  key is stored with `0600` permissions or read from `OPENROUTER_API_KEY`.

## Scope

In scope: the daemon, IPC, the safety guard, the installers, the shell integrations.
Out of scope: what a model suggests when you approve it, and third-party installers you choose to run.
