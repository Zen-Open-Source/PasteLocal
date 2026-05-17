# PasteLocal

![Header](assets/pastelocal-header.png)

**Secure clipboard sharing over SSH for remote work and agentic coding tools.**

PasteLocal lets you access your local clipboard (including screenshots) from any remote machine over SSH — with zero friction. Built for developers who live in the terminal and use agentic coding tools.

---

## Why PasteLocal?

When you SSH into a remote machine (a VPS, server, or cloud instance), your local clipboard becomes unreachable. You take a screenshot or copy important text on your laptop, switch to your terminal, and suddenly you can't paste it into your agentic coding tool (Claude, Cursor, etc.) running on the remote.

This friction is especially painful for developers who rely on AI coding assistants over SSH.

**PasteLocal solves this cleanly:**

- Works over your existing SSH connection — no new ports or services exposed
- Extremely simple remote command (`pastelocal-remote`)
- Excellent integration with agentic coding tools via skills (`/paste`, `/paste-history`, etc.)
- Built-in clipboard history and named snippets
- Works with popular SSH clients like Termius

It’s the most reliable way today to bring your local clipboard into remote development environments.

---

## 60-Second Quick Start

```bash
# 1. Install the CLI (recommended)
go install github.com/Zen-Open-Source/PasteLocal/cmd/pastelocal@latest

# 2. Set up the local daemon and generate a secure token
pastelocal init

# 3. Add your remote server (this edits your SSH config and installs the remote helper)
pastelocal add-host myserver

# 4. SSH into the server and pull your local clipboard
pastelocal-remote
```

`pastelocal-remote` will save the latest clipboard content (including screenshots) to a file on the remote machine and print the path.

You can then use the `/paste` skill (or just read the file) inside your agentic coding tool.

---

## Features

### Core Workflow
- One-command remote clipboard access via SSH tunnel
- Full support for images (screenshots) and text
- Works with any SSH client (including Termius)

### Integration with Agentic Coding Tools
- `/paste` — Paste current clipboard
- `/paste-history` — Choose from recent clipboard entries
- `/paste-snippet` — Recall saved named snippets
- `/paste-send` — Send files from the remote machine back to your local clipboard

### Productivity Tools
- **Clipboard History** — Go back in time with `pastelocal-remote --list`
- **Named Snippets** — Save frequently used text or images locally
- **TUI Dashboard** — Run `pastelocal` to see daemon status, hosts, and recent activity
- **Doctor** — `pastelocal doctor --fix` automatically diagnoses and repairs most issues

### Experimental
- **Multi-device Relay** — E2E encrypted clipboard sync without SSH tunnels (see below)

---

## Installation

### Recommended: Go Install

```bash
go install github.com/Zen-Open-Source/PasteLocal/cmd/pastelocal@latest
```

### Build from Source

```bash
git clone https://github.com/Zen-Open-Source/PasteLocal.git
cd PasteLocal
make build
```

Binaries will be in the `bin/` directory.

> **Note for v0.1.0:** The recommended `go install` command above may not work yet because the Go module path is still being migrated. For the most reliable experience, we recommend cloning the repo and running `make build` instead.

---

## Usage Examples

### Basic Remote Clipboard

```bash
# On your remote server
pastelocal-remote
# → /home/user/.cache/pastelocal/pastelocal-abc123.png
```

### Using with Agentic Coding Tools

The easiest way to use PasteLocal inside tools like Claude, Cursor, Windsurf, and others is to add a custom command/skill.

**Recommended prompt to add:**

```markdown
You have access to the user's local clipboard via PasteLocal.

To retrieve the latest clipboard content (including screenshots):

1. Run the command `pastelocal-remote` in the terminal on this remote machine.
2. It will output a file path (e.g. `~/.cache/pastelocal/pastelocal-xxx.png`).
3. Use your Read tool on that exact file path.
4. After reading the file, run `rm <path>` to clean it up.

Confirm to the user once you've received the clipboard content.
```

This works reliably with most agentic coding tools.

### Clipboard History

```bash
pastelocal-remote --list
pastelocal-remote --list --index 3
```

### Named Snippets

On your local machine:

```bash
pastelocal snippets save api-key
pastelocal snippets save deploy-script --description "Common deploy command"
```

On the remote:

```bash
pastelocal-remote --snippet api-key
```

---

## Experimental: Multi-Device Relay

PasteLocal has an **experimental** relay system that allows clipboard sharing between multiple devices without requiring direct SSH tunnels.

**Current Status:** Experimental / Preview

**What works today:**
- Device pairing with end-to-end encryption (X25519 + AES-GCM)
- Receiving clipboard content from paired peers via `pastelocal-remote --relay`

**What is still in progress:**
- Reliable sending from the local daemon
- Background notifications
- Persistence across relay restarts

**Recommendation:** Use the SSH-based workflow for daily work. The relay is intended for testing and specific multi-machine setups.

---

## How It Works

```
Your Laptop                     Remote Server
┌──────────────┐               ┌─────────────────────┐
│ Local        │               │ Agentic Coding Tool │
│ Clipboard    │◄── SSH ───────│  pastelocal-remote  │
│              │   tunnel      │                     │
│ pastelocald  │               └─────────────────────┘
│ :7331        │
└──────────────┘
```

Everything travels over your existing encrypted SSH connection. No new ports are opened.

---

## Configuration

Configuration lives at `~/.config/pastelocal/config.toml`.

Key options:

```toml
[history]
enabled = true
size = 20
ttl_seconds = 3600

[relay]
enabled = false                    # Experimental
relay_url = "http://localhost:7332"
auto_upload = false
```

Run `pastelocal` (the TUI) or `pastelocal doctor` to inspect your setup.

---

## Troubleshooting

Run this first:

```bash
pastelocal doctor --fix
```

Common issues and fixes are documented in the full docs:
- [Troubleshooting](docs/README.md#troubleshooting-top-10)
- [Error Codes](docs/ERROR_CODES.md)

---

## Contributing

Contributions are very welcome!

- Read [CONTRIBUTING.md](CONTRIBUTING.md)
- Check the [Architecture](docs/ARCHITECTURE.md) document
- Open an issue or pull request

We especially welcome improvements to the relay feature and better agentic coding tool integrations.

---

## Future Vision

PasteLocal already solves the core problem of reliably getting your local clipboard — especially screenshots — into remote agentic coding sessions over SSH.

That said, we have a bigger vision for what this experience could become.

The ideal workflow would feel almost native: paste an image directly in your SSH client (like Termius), have it automatically uploaded to the remote machine with a progress bar, and have the path instantly available in your agentic coding tool.

We see PasteLocal as the foundation for deeper, more seamless integrations with terminals, SSH clients, and agentic coding environments. If you're interested in helping push toward that future, we'd love to hear from you.

---

## Acknowledgments

This project was developed with significant assistance from **Grok Build** (xAI).  
Grok helped with architecture decisions, code reviews, documentation, release configuration, and polishing the project for public launch.

---

## License

MIT © Zen Open Source contributors

---

*Built out of frustration with constantly switching between local screenshots and remote terminals.*
