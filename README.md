# Katzensprung 🐈

![Katzensprung](screenshot.png)

**Share files and folders directly between computers, with friends or across
your own machines.** No account, no cloud, no upload limits. Everything is
end-to-end encrypted with WireGuard.

Katzensprung is a desktop app for Windows, macOS and Linux. It is built on
[tailcat](https://github.com/tailscale/tailcat), which uses Tailscale's open
source data plane (WireGuard, NAT traversal and DERP relays) without a
Tailscale account or control plane.

> *Katzensprung* is German for "a cat's leap". It's the phrase for something
> that's only a stone's throw away.

## How it works

1. **Share:** Drop files or folders onto the window. You can also click
   **Choose files** or open them with Katzensprung from your file manager.
   After a few seconds you get a link like `katzensprung://get/tcp…`.
2. **Send the link** over any messenger or email.
3. **Receive:** Your friend clicks the link and their Katzensprung app
   opens (or they paste the link into the app). The app shows what's coming,
   and nothing is written until they click **Accept**.
4. The files are saved to the download folder, which you can change in the
   top-right corner. Existing files are never overwritten: a new file gets
   a name like `photo (2).jpg` instead.

A link works as long as the share is running and the sender's app is open.
With **One-time link** turned on, the share stops after the first complete
download. You can stop any share at any time with ⏹.

### Features

- Drag and drop files and whole folder trees
- Clickable `katzensprung://` links that open the app, or forward the link
  to the instance that's already running
- Accept/decline prompt that shows the file list before anything is saved
- Live progress, speed and time remaining for both sender and receiver
- A **⚡ Direct** or **Relay** badge that shows how the connection runs
- One-time links
- Multiple friends can download the same share at once
- Minimal black-and-white UI

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
| Windows  | `katzensprung-amd64-installer.exe` (installer) or `katzensprung.exe` (portable) |
| macOS    | `Katzensprung-macOS.zip` (universal `.app`) |
| Linux    | `katzensprung` (needs GTK 3 and WebKitGTK 4.1) |

The builds aren't code-signed. On Windows, SmartScreen shows a warning;
click *More info → Run anyway*. On macOS, right-click the app and choose
*Open* the first time.

### Link registration

| Platform | How `katzensprung://` links are registered |
|----------|-----|
| Windows  | The installer registers them. The portable `.exe` registers itself for the current user on first start, with no admin rights needed. |
| macOS    | Declared in the app's `Info.plist`. Opening the app once is enough. |
| Linux    | On first start the app creates `~/.local/share/applications/katzensprung.desktop` and sets itself as the handler with `xdg-mime`. |

## Development

You need:
- Go 1.27.2 or newer
- Node.js 22
- [Wails v2](https://wails.io), installed with:

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1
```

On Linux you also need `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`:

```bash
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
```

Run in dev mode with hot reload:

```bash
wails dev -tags webkit2_41
```

Build:

```bash
wails build -tags webkit2_41
```

Leave out `-tags webkit2_41` on Windows and macOS. You can also build the
Windows app and its installer from Linux (needs `nsis`):

```bash
wails build -platform windows/amd64 -nsis
```

### Tests

The tests include unit tests and an end-to-end test that shares a file and a
folder over a real tailcat tunnel, using a local DERP relay so no internet
is needed:

```bash
go test -tags webkit2_41 ./...
```

### Own relay / offline testing

Set `KATZENSPRUNG_DERPMAP_URL` to use your own DERP map, for example for a
[self-hosted relay](https://github.com/tailscale/tailscale/tree/main/cmd/derper#derp).
For local testing there's a development relay:

```bash
go run ./tools/devderp
```

```bash
KATZENSPRUNG_DERPMAP_URL=http://127.0.0.1:8475/derpmap.json ./build/bin/katzensprung
```

Both the sender and the receiver need the same setting.

### Command line

```bash
katzensprung photo.jpg Holiday/
```

This starts sharing the given files and prints the link to stdout. Passing
a `katzensprung://` link instead opens it. If the app is already running,
the arguments are forwarded to it.

## Project layout

| Path | Contents |
|------|----------|
| `send.go` | Shares: tailcat server, file collection, sending |
| `receive.go` | Receiving: tailcat client, accept/decline, safe writing |
| `protocol.go` | Transfer protocol and path sanitizing |
| `link.go` | `katzensprung://get/<tailcat address>` links |
| `app.go` | State, progress/speed, settings, dialogs, OS integration |
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
- [Wails](https://wails.io) for the desktop shell
- [Inter](https://rsms.me/inter/) typeface (SIL Open Font License)

Katzensprung is an independent project and is not affiliated with or
endorsed by Tailscale Inc.
