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

## Phone can't reach the QR code / URL?

If your phone is on the same Wi-Fi but the page won't load — especially if you get an error like `ERR_ADDRESS_UNREACHABLE`, or it works on one Wi-Fi network but not another — the most common cause is **AP/client isolation**. This is a router setting (sometimes called "AP Isolation," "Client Isolation," or "Wireless Isolation") that lets every device reach the internet but blocks them from reaching each other directly. It's common on:

- Public or shared Wi-Fi (cafes, offices, hostels, apartment-building routers)
- Guest networks (even ones with a name that looks identical to your main network)
- Some routers with it enabled by default, even at home

**How to tell if this is the issue:** on the phone that can't connect, open a browser and try your router's admin page (usually `http://192.168.1.1` or similar). If that loads but wifiler's URL doesn't, isolation is almost certainly the cause — not wifiler.

**If it's your own router:** log into its admin settings and look for an "AP Isolation" / "Client Isolation" toggle under the Wi-Fi settings, and turn it off. If you don't know the admin login, check the sticker on the router itself for the default credentials, or use your ISP's companion app if they have one.

**If it's a network you don't control** (public Wi-Fi, someone else's router): isolation is usually intentional there for security, and there's no fix on wifiler's end — local peer-to-peer tools like this generally can't work on isolated networks. The workaround is to use a network you control instead, e.g. your phone's own mobile hotspot.

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
