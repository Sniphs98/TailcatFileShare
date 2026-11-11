package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// App is bound to the frontend; its exported methods are callable from JS.
type App struct {
	// desktop is the window-system glue (nil in tests).
	desktop *desktop

	mu        sync.Mutex
	shares    map[string]*share
	transfers map[string]*transfer
	settings  Settings
	dirty     atomic.Bool

	pendingLink  string     // link received before the UI was ready
	pendingShare [][]string // files passed on the command line before the UI was ready
	uiReady      bool
}

type Settings struct {
	DownloadDir string `json:"downloadDir"`
}

// ---- views sent to the frontend ----

type FileView struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type ShareView struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Files     []FileView `json:"files"`
	FileCount int        `json:"fileCount"`
	Total     int64      `json:"total"`
	Link      string     `json:"link"`
	Status    string     `json:"status"` // starting, ready, error, stopped
	Error     string     `json:"error,omitempty"`
	Downloads int        `json:"downloads"`
	Active    int        `json:"active"`
	OneTime   bool       `json:"oneTime"`
	CreatedAt int64      `json:"createdAt"`
}

type TransferView struct {
	ID        string     `json:"id"`
	Direction string     `json:"direction"` // send, recv
	ShareID   string     `json:"shareId,omitempty"`
	Title     string     `json:"title"`
	Peer      string     `json:"peer,omitempty"`
	Status    string     `json:"status"` // connecting, awaiting, waiting, transferring, verifying, done, declined, error, cancelled
	Error     string     `json:"error,omitempty"`
	Done      int64      `json:"done"`
	Total     int64      `json:"total"`
	Speed     float64    `json:"speed"` // bytes/s
	Route     string     `json:"route"` // "", direct, relay
	RouteInfo string     `json:"routeInfo,omitempty"`
	Files     []FileView `json:"files,omitempty"`
	FileCount int        `json:"fileCount"`
	SavedTo   string     `json:"savedTo,omitempty"`
	StartedAt int64      `json:"startedAt"`
}

type State struct {
	Shares    []ShareView    `json:"shares"`
	Transfers []TransferView `json:"transfers"`
	Settings  Settings       `json:"settings"`
	Platform  string         `json:"platform"`
}

// ---- transfer bookkeeping (shared by sender and receiver) ----

type transfer struct {
	TransferView
	done      atomic.Int64
	lastDone  int64
	lastTick  time.Time
	cancel    context.CancelFunc
	decision  chan bool // receiver: user accepted (true) or declined (false)
	finishedT time.Time
}

func NewApp() *App {
	a := &App{
		shares:    map[string]*share{},
		transfers: map[string]*transfer{},
	}
	a.settings = loadSettings()
	return a
}

// ServiceStartup is called by Wails when the app starts.
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	go a.emitLoop()
	go registerProtocol()
	return nil
}

// ServiceShutdown is called by Wails when the app quits.
func (a *App) ServiceShutdown() error {
	a.mu.Lock()
	shares := make([]*share, 0, len(a.shares))
	for _, s := range a.shares {
		shares = append(shares, s)
	}
	transfers := make([]*transfer, 0, len(a.transfers))
	for _, t := range a.transfers {
		transfers = append(transfers, t)
	}
	a.mu.Unlock()
	for _, t := range transfers {
		if t.cancel != nil {
			t.cancel()
		}
	}
	for _, s := range shares {
		s.stop()
	}
	return nil
}

func (a *App) emit(name string, data any) {
	if a.desktop != nil {
		a.desktop.emit(name, data)
	}
}

func (a *App) showWindow() {
	if a.desktop != nil {
		a.desktop.show()
	}
}

func (a *App) toastError(err error) {
	a.emit("toast", map[string]string{"kind": "error", "text": err.Error()})
}

// emitLoop pushes the state to the UI whenever something changed, at most
// five times a second, and keeps the transfer speeds up to date.
func (a *App) emitLoop() {
	tk := time.NewTicker(200 * time.Millisecond)
	defer tk.Stop()
	for range tk.C {
		a.mu.Lock()
		now := time.Now()
		for _, t := range a.transfers {
			d := t.done.Load()
			if t.Status == "transferring" {
				if !t.lastTick.IsZero() {
					dt := now.Sub(t.lastTick).Seconds()
					inst := float64(d-t.lastDone) / dt
					if t.Speed == 0 {
						t.Speed = inst
					} else {
						t.Speed = 0.8*t.Speed + 0.2*inst
					}
				}
				a.dirty.Store(true)
			}
			t.lastDone, t.lastTick = d, now
		}
		a.mu.Unlock()
		if a.dirty.Swap(false) {
			a.emit("state", a.GetState())
		}
	}
}

func (a *App) changed() { a.dirty.Store(true) }

// GetState returns everything the UI renders.
func (a *App) GetState() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := State{Settings: a.settings, Platform: goruntime.GOOS}
	for _, s := range a.shares {
		st.Shares = append(st.Shares, s.view())
	}
	for _, t := range a.transfers {
		v := t.TransferView
		v.Done = t.done.Load()
		st.Transfers = append(st.Transfers, v)
	}
	sort.Slice(st.Shares, func(i, j int) bool { return st.Shares[i].CreatedAt > st.Shares[j].CreatedAt })
	sort.Slice(st.Transfers, func(i, j int) bool { return st.Transfers[i].StartedAt > st.Transfers[j].StartedAt })
	if st.Shares == nil {
		st.Shares = []ShareView{}
	}
	if st.Transfers == nil {
		st.Transfers = []TransferView{}
	}
	return st
}

// UIReady is called by the frontend once it listens for events. It returns
// nothing; a link that started the app is opened now.
func (a *App) UIReady() {
	a.mu.Lock()
	a.uiReady = true
	link := a.pendingLink
	a.pendingLink = ""
	shares := a.pendingShare
	a.pendingShare = nil
	a.mu.Unlock()
	if link != "" {
		a.openLinkFromOS(link)
	}
	if len(shares) > 0 {
		a.showWindow()
	}
	for _, paths := range shares {
		if _, err := a.CreateShare(paths, false); err != nil {
			a.toastError(err)
		}
	}
}

// handleLaunchArgs is called with the arguments of this or a second
// instance: a share link (a clicked link) or files to share ("Open with
// CatJump" in the file manager).
//
// It reports whether the arguments asked for something (a link or files).
func (a *App) handleLaunchArgs(args []string) bool {
	if l := linkFromArgs(args); l != "" {
		a.handleURL(l)
		return true
	}
	var paths []string
	for _, p := range args {
		if strings.HasPrefix(p, "-") {
			continue
		}
		if u, err := url.Parse(p); err == nil && u.Scheme == "file" {
			p = u.Path
		}
		if abs, err := filepath.Abs(p); err == nil {
			if _, err := os.Stat(abs); err == nil {
				paths = append(paths, abs)
			}
		}
	}
	if len(paths) == 0 {
		return false
	}
	a.mu.Lock()
	ready := a.uiReady
	if !ready {
		a.pendingShare = append(a.pendingShare, paths)
	}
	a.mu.Unlock()
	if ready {
		a.showWindow()
		if _, err := a.CreateShare(paths, false); err != nil {
			a.toastError(err)
		}
	}
	return true
}

func (a *App) handleURL(link string) {
	a.mu.Lock()
	if !a.uiReady {
		a.pendingLink = link
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	a.openLinkFromOS(link)
}

func (a *App) openLinkFromOS(link string) {
	a.showWindow()
	if _, err := a.OpenLink(link); err != nil {
		a.toastError(err)
	}
}

func (a *App) newTransfer(dir, title string) *transfer {
	t := &transfer{TransferView: TransferView{
		ID:        uuid.NewString(),
		Direction: dir,
		Title:     title,
		Status:    "connecting",
		StartedAt: time.Now().UnixMilli(),
	}}
	a.mu.Lock()
	a.transfers[t.ID] = t
	a.mu.Unlock()
	a.changed()
	return t
}

// update mutates a transfer under the lock and schedules a UI refresh.
func (a *App) update(t *transfer, f func(t *transfer)) {
	a.mu.Lock()
	f(t)
	switch t.Status {
	case "done", "declined", "error", "cancelled":
		t.Speed = 0
		if t.finishedT.IsZero() {
			t.finishedT = time.Now()
		}
	}
	a.mu.Unlock()
	a.changed()
}

func (a *App) fail(t *transfer, err error) {
	a.update(t, func(t *transfer) {
		if t.Status == "cancelled" || t.Status == "declined" {
			return
		}
		t.Status = "error"
		t.Error = err.Error()
	})
}

func (a *App) getTransfer(id string) (*transfer, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.transfers[id]
	if !ok {
		return nil, errors.New("transfer not found")
	}
	return t, nil
}

// AcceptTransfer lets an incoming transfer proceed.
func (a *App) AcceptTransfer(id string) error { return a.decide(id, true) }

// DeclineTransfer rejects an incoming transfer.
func (a *App) DeclineTransfer(id string) error { return a.decide(id, false) }

func (a *App) decide(id string, ok bool) error {
	t, err := a.getTransfer(id)
	if err != nil {
		return err
	}
	if t.decision == nil {
		return errors.New("no decision pending")
	}
	select {
	case t.decision <- ok:
	default:
	}
	return nil
}

// CancelTransfer aborts a running transfer.
func (a *App) CancelTransfer(id string) error {
	t, err := a.getTransfer(id)
	if err != nil {
		return err
	}
	a.update(t, func(t *transfer) {
		switch t.Status {
		case "done", "declined", "error", "cancelled":
		default:
			t.Status = "cancelled"
		}
	})
	if t.cancel != nil {
		t.cancel()
	}
	return nil
}

// DismissTransfer removes a finished transfer from the list.
func (a *App) DismissTransfer(id string) {
	a.mu.Lock()
	if t, ok := a.transfers[id]; ok && !t.finishedT.IsZero() {
		delete(a.transfers, id)
	}
	a.mu.Unlock()
	a.changed()
}

// ClearFinished removes all finished transfers.
func (a *App) ClearFinished() {
	a.mu.Lock()
	for id, t := range a.transfers {
		if !t.finishedT.IsZero() {
			delete(a.transfers, id)
		}
	}
	a.mu.Unlock()
	a.changed()
}

// ---- dialogs and OS integration ----

// PickFiles opens a file dialog and returns the selected paths.
func (a *App) PickFiles() ([]string, error) {
	if a.desktop == nil {
		return nil, nil
	}
	return a.desktop.openDialog("Choose files to share", "", false).PromptForMultipleSelection()
}

// PickFolder opens a folder dialog for sharing a whole folder.
func (a *App) PickFolder() (string, error) {
	if a.desktop == nil {
		return "", nil
	}
	return a.desktop.openDialog("Choose a folder to share", "", true).PromptForSingleSelection()
}

// ChooseDownloadDir lets the user change where received files go.
func (a *App) ChooseDownloadDir() (Settings, error) {
	a.mu.Lock()
	cur := a.settings.DownloadDir
	a.mu.Unlock()
	if a.desktop == nil {
		return a.settings, nil
	}
	dir, err := a.desktop.openDialog("Where to save received files", cur, true).PromptForSingleSelection()
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil || dir == "" {
		return a.settings, err
	}
	a.settings.DownloadDir = dir
	saveSettings(a.settings)
	a.dirty.Store(true)
	return a.settings, nil
}

// Reveal shows a file or folder in the system file manager.
func (a *App) Reveal(p string) error {
	if p == "" {
		return nil
	}
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", "/select,", p)
	case "darwin":
		cmd = exec.Command("open", "-R", p)
	default:
		dir := p
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			dir = filepath.Dir(p)
		}
		cmd = exec.Command("xdg-open", dir)
	}
	return cmd.Start()
}

// CopyText puts text on the clipboard.
func (a *App) CopyText(s string) error {
	if a.desktop == nil || !a.desktop.app.Clipboard.SetText(s) {
		return errors.New("couldn't copy to the clipboard")
	}
	return nil
}

// HideWindow hides the window back into the tray.
func (a *App) HideWindow() {
	if a.desktop != nil {
		a.desktop.hide()
	}
}

// Quit exits CatJump, stopping all shares.
func (a *App) Quit() {
	if a.desktop != nil {
		a.desktop.app.Quit()
	}
}

// GetAutostart reports whether CatJump starts with the computer.
func (a *App) GetAutostart() bool {
	if a.desktop == nil {
		return false
	}
	on, err := a.desktop.app.Autostart.IsEnabled()
	return err == nil && on
}

// SetAutostart turns starting with the computer on or off.
func (a *App) SetAutostart(on bool) error {
	if a.desktop == nil {
		return nil
	}
	if on {
		return a.desktop.app.Autostart.EnableWithOptions(application.AutostartOptions{Arguments: []string{backgroundFlag}})
	}
	return a.desktop.app.Autostart.Disable()
}

// ---- settings ----

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "CatJump", "config.json")
}

func defaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Downloads")
}

func loadSettings() Settings {
	s := Settings{DownloadDir: defaultDownloadDir()}
	if p := configPath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &s)
		}
	}
	if s.DownloadDir == "" {
		s.DownloadDir = defaultDownloadDir()
	}
	return s
}

func saveSettings(s Settings) {
	p := configPath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(p, b, 0o644)
}

func shortKey(s string) string {
	if len(s) > 16 {
		return fmt.Sprintf("%s…%s", s[8:14], s[len(s)-4:])
	}
	return s
}
