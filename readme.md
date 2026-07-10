# wifiler

A tiny local file server. Run it in a folder, scan the QR code with your phone, and send files back and forth over the same Wi-Fi network — no cloud, no cables, no accounts.

## Features

- **QR code to connect** — scan it with your phone's camera, no app needed
- **Browse & download** files from the folder you launched it in
- **Upload** files from your phone straight into that folder
- **Live sync** — the page updates automatically when files change
- **Session key** — only devices that scan the QR code (or are given the key) can connect
- **Scoped to one folder** — can't browse anywhere outside where you started it
- **One instance at a time** — running it twice on the same machine is blocked

## Install

**macOS / Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/akashkumarpr/wifiler/main/scripts/install.sh | bash
```

**Windows**

```powershell
irm https://raw.githubusercontent.com/akashkumarpr/wifiler/main/scripts/install.ps1 | iex
```

## Usage

1. Open a terminal in the folder you want to share.
2. Run:
   ```bash
   wifiler
   ```
3. A QR code prints in the terminal. Scan it with your phone (make sure it's on the **same Wi-Fi network**).
4. Your phone opens a page where you can browse, download, and upload files.

To stop the server, press `Ctrl+C`.

### Commands & flags

```bash
wifiler                  # share the current directory
wifiler --dir <path>     # share a specific directory instead of the current one
wifiler --port <port>    # try this port first instead of the default (8080)
wifiler version          # print the installed version
wifiler help             # show usage
wifiler uninstall        # remove wifiler from this machine
```

If the requested port is already in use, wifiler automatically picks a free one instead of failing to start.

## Using WSL on Windows?

If you run wifiler inside WSL (Windows Subsystem for Linux), the QR code will usually **not load on your phone**, even on the same Wi-Fi. This isn't a wifiler bug — by default, WSL2 sits behind its own internal network (NAT), so the IP address wifiler finds is only reachable from inside Windows/WSL, not from other devices. wifiler detects this and prints a warning at startup, but here's the fix:

**Enable WSL2 mirrored networking** (Windows 11 22H2 or later):

1. On Windows, create or edit `%UserProfile%\.wslconfig`:
   ```ini
   [wsl2]
   networkingMode=mirrored
   ```
2. Restart WSL:
   ```powershell
   wsl --shutdown
   ```
3. Reopen your WSL terminal and run `wifiler` again — it will now report your real Wi-Fi IP and the QR code will work.

If you're on an older version of Windows without mirrored networking support, the simplest option is to just run `wifiler.exe` directly on Windows instead of inside WSL.

## Uninstall

```bash
wifiler uninstall
```

## Notes on security

- wifiler generates a new random session key every time it starts. The QR code encodes this key, so anyone who scans it (or is told the key) can connect — no key, no access.
- Only the folder you launch wifiler from (and its subfolders) is ever shared. It can't reach parent folders, sibling folders, or other drives.
- Everything is served over plain HTTP, so it's meant for trusted local networks (home Wi-Fi, etc.) — not public or untrusted ones. On an open/public network, anyone sniffing traffic could capture the key.
- Once a device has connected, there's no way to revoke just that device — stopping the server is the only way to cut off access for everyone.
- Each connected device browses independently; one phone opening a subfolder doesn't affect what anyone else sees.
- There's no limit on upload size or count beyond available disk space, so only share access with people you trust not to fill your disk.
- The connection URL contains your session key, so avoid sharing screenshots of it or leaving it in places others can see (like browser history on a shared device).
- Only one wifiler process can run on a machine at a time.

## License

[MIT](LICENSE)
