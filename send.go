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
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tailscale/tailcat"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/logger"
)

// derpRegionOverride, if non-nil, is the DERP relay shares bootstrap
// through instead of the nearest public tailcat relay. Tests set it to a
// local relay.
var derpRegionOverride *tailcfg.DERPRegion

type share struct {
	app *App

	ShareView              // guarded by app.mu
	files     []sourceFile // immutable after creation
	srv       *tailcat.Server
	ln        net.Listener
	ctx       context.Context
	cancel    context.CancelFunc
	stopOnce  sync.Once
}

type sourceFile struct {
	abs  string
	rel  string // slash-separated path the receiver sees
	size int64
}

func (s *share) view() ShareView {
	v := s.ShareView
	if len(v.Files) > 50 {
		v.Files = v.Files[:50]
	}
	return v
}

// CreateShare starts sharing the given files and folders. The returned share
// is in state "starting"; its link appears once the tunnel is up.
func (a *App) CreateShare(paths []string, oneTime bool) (ShareView, error) {
	files, err := collectFiles(paths)
	if err != nil {
		return ShareView{}, err
	}
	var total int64
	views := make([]FileView, 0, len(files))
	for _, f := range files {
		total += f.size
		views = append(views, FileView{Path: f.rel, Size: f.size})
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &share{
		app:    a,
		files:  files,
		ctx:    ctx,
		cancel: cancel,
		ShareView: ShareView{
			ID:        uuid.NewString(),
			Title:     shareTitle(files),
			Files:     views,
			FileCount: len(files),
			Total:     total,
			Status:    "starting",
			OneTime:   oneTime,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	a.mu.Lock()
	a.shares[s.ID] = s
	a.mu.Unlock()
	a.changed()
	go s.run()
	return s.view(), nil
}

// StopShare ends a share; its link stops working and running downloads abort.
func (a *App) StopShare(id string) error {
	a.mu.Lock()
	s, ok := a.shares[id]
	a.mu.Unlock()
	if !ok {
		return errors.New("freigabe nicht gefunden")
	}
	s.stop()
	return nil
}

// RemoveShare stops a share and removes it from the list.
func (a *App) RemoveShare(id string) error {
	if err := a.StopShare(id); err != nil {
		return err
	}
	a.mu.Lock()
	delete(a.shares, id)
	a.mu.Unlock()
	a.changed()
	return nil
}

func (s *share) setStatus(status, errText string) {
	s.app.mu.Lock()
	if s.Status != "stopped" {
		s.Status = status
		s.Error = errText
	}
	s.app.mu.Unlock()
	s.app.changed()
}

func (s *share) stop() {
	s.stopOnce.Do(func() {
		s.app.mu.Lock()
		s.Status = "stopped"
		s.Link = ""
		s.app.mu.Unlock()
		s.cancel()
		if s.ln != nil {
			s.ln.Close()
		}
		if s.srv != nil {
			s.srv.Close()
		}
		s.app.changed()
	})
}

func (s *share) run() {
	srv := &tailcat.Server{
		Logf:       logger.Discard,
		Region:     derpRegionOverride,
		DERPMapURL: os.Getenv("KATZENSPRUNG_DERPMAP_URL"),
	}
	ctx, cancel := context.WithTimeout(s.ctx, 45*time.Second)
	ln, err := srv.Listen(ctx, "tcp", fmt.Sprintf(":%d", transferPort))
	cancel()
	if err != nil {
		srv.Close()
		if s.ctx.Err() == nil {
			s.setStatus("error", "Verbindung zum Relay fehlgeschlagen: "+err.Error())
		}
		return
	}
	s.app.mu.Lock()
	stopped := s.Status == "stopped"
	if !stopped {
		s.srv, s.ln = srv, ln
		s.Link = makeLink(srv.TailcatAddr())
		s.Status = "ready"
		fmt.Printf("Freigabe %q bereit: %s\n", s.Title, s.Link)
	}
	s.app.mu.Unlock()
	if stopped {
		ln.Close()
		srv.Close()
		return
	}
	s.app.changed()

	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go s.serve(c)
	}
}

func (s *share) serve(c net.Conn) {
	a := s.app
	defer c.Close()

	t := a.newTransfer("send", s.Title)
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	go func() { <-ctx.Done(); c.Close() }()

	peer, _ := s.srv.PeerKey(c.RemoteAddr())
	a.update(t, func(t *transfer) {
		t.ShareID = s.ID
		t.Total = s.Total
		t.FileCount = len(s.files)
		t.Peer = shortKey(peer.String())
		t.cancel = cancel
		t.Status = "waiting"
	})
	a.mu.Lock()
	s.Active++
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		s.Active--
		a.mu.Unlock()
		a.changed()
	}()
	go s.watchRoute(ctx, t, peer)

	err := s.send(ctx, c, t)
	switch {
	case err == nil:
		a.update(t, func(t *transfer) { t.Status = "done" })
		a.mu.Lock()
		s.Downloads++
		once := s.OneTime
		a.mu.Unlock()
		if once {
			// Give the receiver a moment to close its side first.
			time.AfterFunc(2*time.Second, s.stop)
		}
	case errors.Is(err, errDeclined):
		a.update(t, func(t *transfer) { t.Status = "declined" })
	case ctx.Err() != nil:
		a.update(t, func(t *transfer) {
			if t.Status != "cancelled" {
				t.Status = "cancelled"
				t.Error = "Freigabe beendet"
			}
		})
	default:
		a.fail(t, err)
	}
}

var errDeclined = errors.New("vom Empfänger abgelehnt")

func (s *share) send(ctx context.Context, c net.Conn, t *transfer) error {
	m := manifest{Version: protocolVersion, Title: s.Title, Total: s.Total}
	for _, f := range s.files {
		m.Files = append(m.Files, manifestFile{Path: f.rel, Size: f.size})
	}
	if err := writeLine(c, m); err != nil {
		return fmt.Errorf("Empfänger nicht erreichbar: %w", err)
	}
	br := bufio.NewReader(c)
	var r reply
	if err := readLine(br, &r); err != nil {
		if errors.Is(err, io.EOF) {
			return errDeclined
		}
		return fmt.Errorf("Verbindung abgebrochen: %w", err)
	}
	if !r.Accept {
		return errDeclined
	}
	s.app.update(t, func(t *transfer) { t.Status = "transferring" })

	bw := bufio.NewWriterSize(&countingWriter{w: c, t: t}, 256<<10)
	for _, f := range s.files {
		if err := copyFile(bw, f); err != nil {
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("Übertragung abgebrochen: %w", err)
	}
	s.app.update(t, func(t *transfer) { t.Status = "verifying" })
	var d doneMsg
	if err := readLine(br, &d); err != nil {
		return fmt.Errorf("keine Bestätigung vom Empfänger: %w", err)
	}
	if !d.OK {
		return fmt.Errorf("Empfänger meldet Fehler: %s", d.Error)
	}
	return nil
}

func copyFile(w io.Writer, f sourceFile) error {
	fh, err := os.Open(f.abs)
	if err != nil {
		return fmt.Errorf("%s kann nicht gelesen werden: %w", f.rel, err)
	}
	defer fh.Close()
	n, err := io.CopyN(w, fh, f.size)
	if err != nil {
		if n < f.size && (errors.Is(err, io.EOF)) {
			return fmt.Errorf("%s wurde während des Sendens verkleinert", f.rel)
		}
		return fmt.Errorf("Übertragung abgebrochen: %w", err)
	}
	return nil
}

type countingWriter struct {
	w io.Writer
	t *transfer
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.t.done.Add(int64(n))
	return n, err
}

// watchRoute reports whether the tunnel to peer runs directly or via a relay.
func (s *share) watchRoute(ctx context.Context, t *transfer, peer key.NodePublic) {
	tk := time.NewTicker(time.Second)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
		st := s.srv.Status()
		ps, ok := st.Peer[peer]
		if !ok {
			continue
		}
		route, info := "relay", ps.Relay
		if ps.CurAddr != "" {
			route, info = "direct", ps.CurAddr
		}
		s.app.update(t, func(t *transfer) { t.Route, t.RouteInfo = route, info })
	}
}

// collectFiles expands the given paths (files and folders) into the list of
// regular files to send, with the relative names the receiver will see.
func collectFiles(paths []string) ([]sourceFile, error) {
	if len(paths) == 0 {
		return nil, errors.New("keine Dateien ausgewählt")
	}
	var out []sourceFile
	usedTop := map[string]bool{}
	for _, p := range paths {
		p = filepath.Clean(p)
		fi, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		top := uniqueName(cleanComponent(filepath.Base(p)), usedTop)
		if top == "" {
			top = uniqueName("Datei", usedTop)
		}
		if !fi.IsDir() {
			if !fi.Mode().IsRegular() {
				return nil, fmt.Errorf("%s ist keine normale Datei", filepath.Base(p))
			}
			out = append(out, sourceFile{abs: p, rel: top, size: fi.Size()})
			continue
		}
		err = filepath.WalkDir(p, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			info, err := os.Stat(fp) // follows symlinks to files
			if err != nil || !info.Mode().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(p, fp)
			if err != nil {
				return err
			}
			r, err := sanitizeRelPath(path.Join(top, filepath.ToSlash(rel)))
			if err != nil {
				return nil
			}
			out = append(out, sourceFile{abs: fp, rel: r, size: info.Size()})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, errors.New("die Auswahl enthält keine Dateien")
	}
	// Two source names can map to the same sanitized name.
	seen := map[string]bool{}
	for _, f := range out {
		k := strings.ToLower(f.rel)
		if seen[k] {
			return nil, fmt.Errorf("doppelter Dateiname %q", f.rel)
		}
		seen[k] = true
	}
	return out, nil
}

func uniqueName(name string, used map[string]bool) string {
	if name == "" {
		return ""
	}
	cand := name
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; used[strings.ToLower(cand)]; i++ {
		cand = fmt.Sprintf("%s (%d)%s", stem, i, ext)
	}
	used[strings.ToLower(cand)] = true
	return cand
}

func shareTitle(files []sourceFile) string {
	tops := []string{}
	seen := map[string]bool{}
	for _, f := range files {
		top := strings.SplitN(f.rel, "/", 2)[0]
		if !seen[top] {
			seen[top] = true
			tops = append(tops, top)
		}
	}
	switch len(tops) {
	case 1:
		return tops[0]
	case 2:
		return tops[0] + " und " + tops[1]
	default:
		return fmt.Sprintf("%s und %d weitere", tops[0], len(tops)-1)
	}
}
