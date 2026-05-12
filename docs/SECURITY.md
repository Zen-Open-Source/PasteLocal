# Security Threat Model

## What We Defend Against

**Unauthorized clipboard access.** Any process on the remote host that can
reach `127.0.0.1:7331` could try to read your clipboard. clipbridge requires
a Bearer token on every `/clipboard` request, compared with constant-time
comparison to prevent timing attacks.

**Lateral access from non-loopback sources.** The daemon binds to
`127.0.0.1` only and rejects non-loopback connections at the TCP level. Even
if the port were exposed on a LAN interface, the connection is closed
immediately.

**Token leakage via process metadata.** The token is never in argv or
environ. `clipbridge-remote` reads it from `~/.config/clipbridge/token`
(mode 0600). The daemon reads from the OS keychain with a file fallback.
Log output is token-redacted.

**Brute-force token guessing.** Tokens are 32-byte random values from
`crypto/rand` (256 bits of entropy). At the default 60 req/min rate limit,
brute-forcing is infeasible.

**Resource exhaustion.** The daemon enforces concurrency limits
(`max_in_flight`), rate limiting (`rate_limit_per_minute`), and size limits
(`max_image_bytes`) to prevent OOM.

**Stale tokens after rotation.** `clipbridge rotate-token` replaces the
token. SIGHUP triggers an immediate re-read; old tokens are rejected within
seconds.

## What We Don't Defend Against

**A compromised remote host.** If an attacker has root on the remote, they
can read the token file and exfiltrate clipboard contents. clipbridge assumes
the remote is trusted — you control both ends.

**Eavesdropping inside the tunnel.** The SSH tunnel is encrypted, but once
traffic exits on the remote side, it travels over loopback HTTP in cleartext.
A root-level process can sniff loopback traffic.

**Malware on the local machine.** If the local machine is compromised, the
attacker can read the clipboard directly — no clipbridge hardening helps.

## Why No E2E Crypto Inside the Tunnel

The data path is: local clipboard → `clipbridged` (loopback HTTP) → SSH
tunnel → `clipbridge-remote` (loopback HTTP) → disk. Both HTTP legs are on
loopback. The SSH tunnel is already encrypted. Adding another crypto layer
inside would double CPU cost with zero benefit: only root on either endpoint
could intercept plaintext, and root already has clipboard or token access.
We choose simplicity over security theater.

## Token Storage Approach

| Platform | Primary Storage | Fallback |
|---|---|---|
| macOS | Keychain (`security add-generic-password`) | `~/.config/clipbridge/token` (0600) |
| Linux | libsecret (via `secret-tool`) | `~/.config/clipbridge/token` (0600) |
| Other | — | `~/.config/clipbridge/token` (0600) |

The daemon prefers the OS keychain, falling back to the file if unavailable.
`clipbridge doctor` verifies both stores and can auto-fix missing entries and
incorrect permissions. Use `--no-keychain` on `clipbridge init` to disable
keychain use entirely.
