# Termius Setup Guide

Termius is a popular SSH client with desktop (macOS, Windows, Linux) and
mobile (iOS, Android) apps. Unlike OpenSSH, Termius does not use
`~/.ssh/config`, so clipbridge cannot auto-edit your SSH configuration.
Instead, you configure the Remote port forwarding manually inside the
Termius UI.

> **Note:** The Termius UI may change in future versions. The menu names and
> field labels below are accurate as of the current release. If you can't find
> a setting, look for similarly-named options in the port forwarding or
> advanced settings section.

---

## Termius Desktop (macOS / Windows / Linux)

### Step 1: Initialize clipbridge locally

```bash
clipbridge init
```

This starts the daemon on port 7331 (default) and stores the auth token.

### Step 2: Get the Termius instructions

```bash
clipbridge add-host myserver --termius
```

This prints the exact values you need to enter in Termius. The output looks
like:

```
Open Termius → Hosts → myserver → Edit → Advanced → Port Forwarding
Add a new entry:
  Type: Remote
  Local: 127.0.0.1
  Local Port: 7331
  Remote: 127.0.0.1
  Remote Port: 7331
Then run: clipbridge add-host myserver --finish
```

### Step 3: Configure port forwarding in Termius

1. Open **Termius**.
2. Go to **Hosts** in the left sidebar.
3. Select your remote host (e.g., `myserver`).
4. Click **Edit** (pencil icon or right-click → Edit).
5. Expand the **Advanced** section.
6. Find **Port Forwarding**.
7. Click **Add** or **+** to create a new forwarding rule.
8. Fill in the fields:

   | Field | Value |
   |-------|-------|
   | **Type** | Remote |
   | **Local Address** | `127.0.0.1` |
   | **Local Port** | `7331` (or your custom port) |
   | **Remote Address** | `127.0.0.1` |
   | **Remote Port** | `7331` (or your custom port) |

9. Save the host configuration.

### Step 4: Finish the installation

```bash
clipbridge add-host myserver --finish
```

This copies the `clipbridge-remote` binary, the auth token, and the Claude
skill file to the remote host via SCP. It skips the SSH config edit (since
Termius handles that part).

### Step 5: Connect and test

1. Connect to the host in Termius. The port forwarding is established
   automatically when the SSH session starts.
2. On the remote host, run:

```bash
clipbridge-remote
```

If you have an image on your local clipboard, this prints a file path. If it
prints "tunnel not connected: connection refused", the port forwarding isn't
active yet — reconnect in Termius.

---

## Termius Mobile (iOS / Android)

### Step 1: Initialize clipbridge locally

On your laptop (not the phone), run:

```bash
clipbridge init
clipbridge add-host myserver --termius
```

Note the port values printed (default: 7331).

### Step 2: Configure port forwarding in Termius Mobile

1. Open **Termius** on your phone.
2. Go to **Hosts**.
3. Tap your remote host.
4. Tap **Edit** (gear or pencil icon).
5. Scroll to **Port Forwarding** or **Advanced**.
6. Tap **Add** to create a new rule.
7. Fill in the fields:

   | Field | Value |
   |-------|-------|
   | **Type** | Remote |
   | **Local Address** | `127.0.0.1` |
   | **Local Port** | `7331` |
   | **Remote Address** | `127.0.0.1` |
   | **Remote Port** | `7331` |

8. Save the host configuration.

### Step 3: Finish the installation

On your laptop, run:

```bash
clipbridge add-host myserver --finish
```

### Step 4: Connect and test

1. Connect to the host in the Termius mobile app.
2. On the remote host, run `clipbridge-remote` to test.

> **Note on mobile:** The "local" side of the RemoteForward is your phone,
> which may not have a running `clipbridged` daemon. For the mobile workflow,
> you typically use a desktop machine as the local clipboard source and the
> phone only for initiating the SSH connection. The daemon must be running on
> the machine that has the clipboard you want to share.

---

## Troubleshooting Termius-Specific Issues

### "tunnel not connected: connection refused"

The port forwarding isn't active. This means either:

- You haven't connected to the host in Termius yet. **Fix:** Open an SSH
  session to the host first.
- The port forwarding rule isn't saved or was configured incorrectly.
  **Fix:** Double-check all five fields (Type, Local Address, Local Port,
  Remote Address, Remote Port) against the values from `clipbridge add-host
  <alias> --termius`.
- You're using a non-default port. **Fix:** Make sure the port in Termius
  matches the port in `~/.config/clipbridge/config.toml`.

### "CB2001: Invalid auth token"

The token on the remote host doesn't match the local token. This happens if
you ran `clipbridge init` again or `clipbridge rotate-token` without
updating the remote.

**Fix:**

```bash
clipbridge add-host myserver --update-token-only
```

### Port forwarding doesn't persist between connections

Termius saves port forwarding rules per-host, so they should persist. If they
don't:

- Check that you saved the host edit after adding the rule.
- Try deleting and re-adding the rule.
- On mobile, make sure you're editing the correct host entry.

### clipbridge-remote not found on remote host

The `--finish` step didn't complete or the binary was removed.

**Fix:**

```bash
clipbridge add-host myserver --finish
```

### Can I use Termius with a non-default port?

Yes. If you changed the port in `config.toml` or used `--port` during
`clipbridge init`, use that same port in both the Local Port and Remote Port
fields in Termius. The two must always match.

### Multiple Termius hosts

Each host needs its own port forwarding rule. Run `clipbridge add-host
<alias> --termius` for each host and configure the rule in the corresponding
Termius host entry.
