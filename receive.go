package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/tailscale/tailcat"
	"tailscale.com/types/logger"
)

// OpenLink connects to the sender behind a share link (or bare tailcat
// address) and shows what it offers. Nothing is written until the user
// accepts with AcceptTransfer.
func (a *App) OpenLink(link string) (string, error) {
	addr, err := parseLink(link)
	if err != nil {
		return "", err
	}
	t := a.newTransfer("recv", "Connecting …")
	ctx, cancel := context.WithCancel(context.Background())
	a.update(t, func(t *transfer) {
		t.cancel = cancel
		t.decision = make(chan bool, 1)
	})
	go a.receive(ctx, t, addr)
	return t.ID, nil
}

func (a *App) receive(ctx context.Context, t *transfer, addr tailcat.Addr) {
	defer t.cancel()
	c := &tailcat.Client{
		Server:     addr,
		Logf:       logger.Discard,
		DERPMapURL: os.Getenv("CATJUMP_DERPMAP_URL"),
	}
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		c.DrainTCP(dctx)
		cancel()
		c.Close()
	}()

	dctx, dcancel := context.WithTimeout(ctx, 45*time.Second)
	conn, err := c.DialTCPPort(dctx, transferPort)
	dcancel()
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		a.fail(t, errors.New("Can't reach the sender. The share may have ended or their computer is offline"))
		return
	}
	defer conn.Close()
	go func() { <-ctx.Done(); conn.Close() }()
	go a.watchClientRoute(ctx, t, c)

	if err := a.receiveOn(ctx, t, conn); err != nil {
		if ctx.Err() != nil {
			a.update(t, func(t *transfer) {
				if t.Status != "declined" {
					t.Status = "cancelled"
				}
			})
			return
		}
		a.fail(t, err)
	}
}

func (a *App) receiveOn(ctx context.Context, t *transfer, conn net.Conn) error {
	br := bufio.NewReaderSize(conn, 256<<10)
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	var m manifest
	if err := readLine(br, &m); err != nil {
		return fmt.Errorf("no response from the sender: %w", err)
	}
	conn.SetReadDeadline(time.Time{})
	if err := m.validate(); err != nil {
		return err
	}
	files := make([]FileView, 0, min(len(m.Files), 200))
	for _, f := range m.Files[:min(len(m.Files), 200)] {
		files = append(files, FileView{Path: f.Path, Size: f.Size})
	}
	a.update(t, func(t *transfer) {
		t.Title = m.Title
		if t.Title == "" {
			t.Title = shareTitleFromManifest(m)
		}
		t.Total = m.Total
		t.FileCount = len(m.Files)
		t.Files = files
		t.Status = "awaiting"
	})
	a.showWindow()

	var ok bool
	select {
	case ok = <-t.decision:
	case <-ctx.Done():
		return ctx.Err()
	}
	if !ok {
		a.update(t, func(t *transfer) { t.Status = "declined" })
		writeLine(conn, reply{Accept: false})
		return nil
	}

	a.mu.Lock()
	dir := a.settings.DownloadDir
	a.mu.Unlock()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("download folder unavailable: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("download folder unavailable: %w", err)
	}
	defer root.Close()

	// Give every top-level entry a name that doesn't exist yet, so nothing
	// already in the download folder is ever overwritten.
	topMap := map[string]string{}
	used := map[string]bool{}
	for _, f := range m.Files {
		top := strings.SplitN(f.Path, "/", 2)[0]
		if _, done := topMap[top]; done {
			continue
		}
		topMap[top] = freeName(root, top, used)
	}
	target := func(p string) string {
		parts := strings.SplitN(p, "/", 2)
		parts[0] = topMap[parts[0]]
		return path.Join(parts...)
	}

	if err := writeLine(conn, reply{Accept: true}); err != nil {
		return fmt.Errorf("connection lost: %w", err)
	}
	a.update(t, func(t *transfer) { t.Status = "transferring" })

	var written []string
	cleanup := func() {
		for _, p := range written {
			root.Remove(p)
		}
	}
	for _, f := range m.Files {
		dst := target(f.Path)
		if d := path.Dir(dst); d != "." {
			if err := root.MkdirAll(filepath.FromSlash(d), 0o755); err != nil {
				cleanup()
				return fmt.Errorf("can't create folder %s: %w", d, err)
			}
		}
		part := filepath.FromSlash(dst + ".catjump-part")
		out, err := root.OpenFile(part, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			cleanup()
			return fmt.Errorf("can't save %s: %w", dst, err)
		}
		written = append(written, part)
		_, err = io.CopyN(&countingWriter{w: out, t: t}, br, f.Size)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			cleanup()
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return errors.New("lost connection to the sender")
			}
			return fmt.Errorf("transfer aborted: %w", err)
		}
		final := filepath.FromSlash(dst)
		if err := root.Rename(part, final); err != nil {
			cleanup()
			return fmt.Errorf("can't save %s: %w", dst, err)
		}
		written[len(written)-1] = final
	}
	writeLine(conn, doneMsg{OK: true})

	saved := dir
	if len(topMap) == 1 {
		for _, v := range topMap {
			saved = filepath.Join(dir, filepath.FromSlash(v))
		}
	}
	a.update(t, func(t *transfer) {
		t.Status = "done"
		t.SavedTo = saved
	})
	// Let the sender read our confirmation before the tunnel goes away.
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	io.Copy(io.Discard, br)
	return nil
}

// freeName returns name, or "name (2).ext", "name (3).ext", … whichever does
// not exist in root yet.
func freeName(root *os.Root, name string, used map[string]bool) string {
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if ext == name { // ".bashrc"
		stem, ext = name, ""
	}
	cand := name
	for i := 2; ; i++ {
		_, err := root.Lstat(cand)
		if errors.Is(err, fs.ErrNotExist) && !used[strings.ToLower(cand)] {
			used[strings.ToLower(cand)] = true
			return cand
		}
		cand = fmt.Sprintf("%s (%d)%s", stem, i, ext)
	}
}

func shareTitleFromManifest(m manifest) string {
	if len(m.Files) == 1 {
		return path.Base(m.Files[0].Path)
	}
	return fmt.Sprintf("%d files", len(m.Files))
}

// watchClientRoute pings the sender periodically. Besides telling the UI
// whether the path is direct or relayed, the pings drive NAT traversal so
// the transfer upgrades from the relay to a direct path when possible.
func (a *App) watchClientRoute(ctx context.Context, t *transfer, c *tailcat.Client) {
	for {
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		r, err := c.DiscoPing(pctx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		wait := 2 * time.Second
		if err == nil {
			route, info := "relay", r.DERPRegionCode
			if r.Endpoint != "" {
				route, info = "direct", r.Endpoint
				wait = 10 * time.Second
			}
			a.update(t, func(t *transfer) { t.Route, t.RouteInfo = route, info })
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}
