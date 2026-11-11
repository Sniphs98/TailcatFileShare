# CatJump 🐈

![CatJump](screenshot.png)

**Share files and folders directly between computers, with friends or across
your own machines.** No account, no cloud, no upload limits. Everything is
end-to-end encrypted with WireGuard.

CatJump is a small tray app for Windows, macOS and Linux: it lives in the
system tray (the menu bar on macOS) and opens a compact window when you
click its icon. It is built on
[tailcat](https://github.com/tailscale/tailcat), which uses Tailscale's open
source data plane (WireGuard, NAT traversal and DERP relays) without a
Tailscale account or control plane.


## How it works

1. **Share:** Click the cat in your tray and drop files or folders onto the
   window. You can also click **Files**/**Folder**, pick **Share files…** from
   the tray icon's menu, or open files with CatJump from your file manager.
   After a few seconds you get a link like `catjump://get/tcp…`.
2. **Send the link** over any messenger or email.
3. **Receive:** Your friend clicks the link and their CatJump app
   pops up from the tray (or they paste the link into the app). The app shows what's coming,
   and nothing is written until they click **Accept**.
4. The files are saved to your download folder, which you can change under
   **Settings**. Existing files are never overwritten: a new file gets
   a name like `photo (2).jpg` instead.

A link works as long as the share is running and CatJump runs on the
sender's computer. Closing the window (✕ or Esc) only hides it in the tray,
so shares stay online; **Quit CatJump** in the tray menu or in Settings
stops everything.
With **One-time link** turned on, the share stops after the first complete
download. You can stop any share at any time with ⏹.

### Features

- Drag and drop files and whole folder trees
- Clickable `catjump://` links that open the app, or forward the link
  to the instance that's already running
- Accept/decline prompt that shows the file list before anything is saved
- Live progress, speed and time remaining for both sender and receiver
- A **⚡ Direct** or **Relay** badge that shows how the connection runs
- One-time links
- Multiple friends can download the same share at once
- Lives in the system tray / menu bar with a compact 400 × 620 window, no
  taskbar or Dock clutter
- Optional start with your computer (Settings), quietly in the tray
- Minimal black-and-white UI with **Send**, **Receive** and **Activity**
  tabs; a clicked link jumps straight to **Receive**

### Connection

The connection starts through a DERP relay (by default the free,
rate-limited [tailcat relays](https://tailcat.dev/derpmap.json)). tailcat
then uses NAT traversal to upgrade to a direct peer-to-peer connection,
which usually succeeds. The relay only forwards encrypted WireGuard packets
and never sees the contents of your files.

## Security

**The link is the key.** Anyone who has the link can download the files
while the share is running. Share links privately, and stop shares you no
longer need.

On the receiving side:

- Nothing is saved until you click **Accept**.
- File names from the sender are sanitized. Absolute paths, `..` and names
  that are invalid on Windows are rejected or fixed.
- Files are only written inside the download folder, enforced with Go's
  [`os.Root`](https://pkg.go.dev/os#Root). They're written as `.part` files
  first and renamed when complete.

Note that tailcat describes itself as experimental; see its
[SECURITY.md](https://github.com/tailscale/tailcat/blob/main/SECURITY.md).

## Download

Builds for every platform come from the GitHub Actions workflow in
[`.github/workflows/build.yml`](.github/workflows/build.yml):

| Platform | Artifact |
|----------|----------|
| Windows  | `catjump-amd64-installer.exe` (installer) or `catjump.exe` (portable) |
| macOS    | `CatJump-macOS.zip` (universal `.app`) |
| Linux    | `catjump` (needs GTK 4 and WebKitGTK 6.0) |

The builds aren't code-signed. On Windows, SmartScreen shows a warning;
click *More info → Run anyway*. On macOS, right-click the app and choose
*Open* the first time.

On Linux, the tray icon needs a desktop with StatusNotifier support (KDE,
Cinnamon, XFCE; on GNOME the *AppIndicator* extension). Without it, launch
CatJump again to bring the window back.

### Link registration

| Platform | How `catjump://` links are registered |
|----------|-----|
| Windows  | The installer registers them. The portable `.exe` registers itself for the current user on first start, with no admin rights needed. |
| macOS    | Declared in the app's `Info.plist`. Opening the app once is enough. |
| Linux    | On first start the app creates `~/.local/share/applications/catjump.desktop` and sets itself as the handler with `xdg-mime`. |

## Development

You need:
- Go 1.27.2 or newer
- Node.js 22
- [Wails v3](https://v3.wails.io), installed with:

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
```

On Linux you also need GTK 4 and WebKitGTK 6.0:

```bash
sudo apt install libgtk-4-dev libwebkitgtk-6.0-dev
```

Run in dev mode with hot reload:

```bash
wails3 dev
```

Build for the current platform (output in `bin/`):

```bash
wails3 build
```

Package the Windows app and its installer, also from Linux (needs `nsis`):

```bash
wails3 task windows:package ARCH=amd64 INSTALL_SCOPE=user
```

Package a universal macOS app (on a Mac):

```bash
wails3 task darwin:package:universal
```

### Tests

The tests include unit tests and an end-to-end test that shares a file and a
folder over a real tailcat tunnel, using a local DERP relay so no internet
is needed:

```bash
go test ./...
```

### Own relay / offline testing

Set `CATJUMP_DERPMAP_URL` to use your own DERP map, for example for a
[self-hosted relay](https://github.com/tailscale/tailscale/tree/main/cmd/derper#derp).
For local testing there's a development relay:

```bash
go run ./tools/devderp
```

```bash
CATJUMP_DERPMAP_URL=http://127.0.0.1:8475/derpmap.json ./bin/catjump
```

Both the sender and the receiver need the same setting.

### Command line

```bash
catjump photo.jpg Holiday/
```

This starts sharing the given files and prints the link to stdout. Passing
a `catjump://` link instead opens it. If the app is already running,
the arguments are forwarded to it.

## Project layout

| Path | Contents |
|------|----------|
| `send.go` | Shares: tailcat server, file collection, sending |
| `receive.go` | Receiving: tailcat client, accept/decline, safe writing |
| `protocol.go` | Transfer protocol and path sanitizing |
| `link.go` | `catjump://get/<tailcat address>` links |
| `app.go` | State, progress/speed, settings, the methods the UI calls |
| `desktop.go` | Tray icon and menu, the compact window, dialogs |
| `register_*.go` | Per-OS registration of the link scheme |
| `frontend/` | UI (vanilla JS + Vite) |
| `tools/devderp/` | Local DERP relay for development |

### Protocol

The protocol uses one TCP connection to port 4747 inside the WireGuard
tunnel:

```
sender   → receiver   manifest   {"v":1,"title":…,"files":[{"path":…,"size":…}],"total":…}
receiver → sender     reply      {"accept":true}
sender   → receiver   the bytes of every file, back to back, exactly "size" bytes each
receiver → sender     done       {"ok":true}
```

## Credits

- [tailcat](https://github.com/tailscale/tailcat) and
  [Tailscale](https://github.com/tailscale/tailscale) (BSD-3-Clause) for
  the networking
- [Wails v3](https://v3.wails.io) for the desktop shell and tray
- [Inter](https://rsms.me/inter/) typeface (SIL Open Font License)

CatJump is an independent project and is not affiliated with or
endorsed by Tailscale Inc.
