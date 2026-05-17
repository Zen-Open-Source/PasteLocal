# pastelocal

**Your clipboard, everywhere.**

Secure, fast clipboard sharing between your local machine and remote hosts over SSH — built for developers who live in the terminal and use tools like Claude.

---

## 30-Second Start

```bash
# 1. Install
go install github.com/pastelocal/pastelocal/cmd/pastelocal@latest

# 2. Initialize (generates token, installs daemon)
pastelocal init

# 3. Add a remote host
pastelocal add-host myserver

# 4. On the remote, just run:
pastelocal-remote
```

PasteLocal copies your local clipboard (including screenshots) to any remote machine via an encrypted SSH tunnel. Works great with Claude, Cursor, and any terminal-based workflow.

---

## Features

- **One-command remote clipboard** — `pastelocal-remote` just works over any SSH connection
- **Claude skills** — `/paste`, `/paste-history`, `/paste-snippet`, `/paste-send`
- **Clipboard history** — Go back to previous copies with `--list` and `--index`
- **Named snippets** — Save and recall frequently used text or images
- **TUI dashboard** — `pastelocal` shows daemon status, hosts, and recent activity
- **Doctor + auto-fix** — `pastelocal doctor --fix` diagnoses and repairs most issues
- **Termius support** — Works with Termius and other SSH clients
- **Experimental: Multi-device relay** — E2E encrypted clipboard sync between machines without SSH tunnels (see below)

---

## Experimental: Relay Mode

PasteLocal includes an experimental relay server for clipboard sharing between multiple devices without requiring persistent SSH tunnels.

**Status:** Experimental / Preview

- Device pairing with X25519 + AES-GCM end-to-end encryption
- Works today for receiving clipboard content from paired peers
- Send path and full daemon integration are still in progress

Use at your own risk. The core SSH path is the recommended, production-ready experience.

---

## Installation

### From source (recommended for now)

```bash
go install github.com/pastelocal/pastelocal/cmd/pastelocal@latest
```

### Build from source

```bash
git clone https://github.com/pastelocal/pastelocal.git
cd pastelocal
make build
```

Binaries will be in the `bin/` directory.

---

## How It Works

```
Local Machine                  Remote Host
┌──────────────┐              ┌────────────────────┐
│  Your OS     │              │  Claude / Terminal │
│  Clipboard   │◄── SSH ──────│  pastelocal-remote │
│              │   tunnel     │                    │
│ pastelocald  │              │  ~/.cache/pastelocal/│
│  :7331       │              └────────────────────┘
└──────────────┘
```

The daemon only listens on loopback. All traffic travels over your existing encrypted SSH connection.

---

## Documentation

- [Full Documentation](docs/README.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Security Model](docs/SECURITY.md)
- [Error Codes](docs/ERROR_CODES.md)
- [Termius Setup](docs/TERMIUS.md)

---

## Status

PasteLocal is actively used in production by the author for daily remote work with Claude.

The core SSH clipboard bridge is stable. History, snippets, and the TUI are solid. The multi-device relay is experimental and under active development.

---

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, testing, and how to submit changes.

---

## License

MIT © pastelocal contributors

---

## Acknowledgments

Built out of frustration with constantly switching between local screenshots and remote terminals. Special thanks to everyone who has dealt with "I can't paste this here."

---

*Clipboard infrastructure for people who ssh.*