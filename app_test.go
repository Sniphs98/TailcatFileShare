package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tailscale.com/envknob"
	"tailscale.com/net/netcheck"
	"tailscale.com/syncs"
	"tailscale.com/tailcfg"
	"tailscale.com/tstest/integration"
	"tailscale.com/types/logger"
)

// TestMain keeps the tests hermetic, like tailcat's own: no portmapper or
// captive-portal probes of the real network.
func TestMain(m *testing.M) {
	envknob.Setenv("IN_TS_TEST", "true")
	netcheck.HookStartCaptivePortalDetection.SetForTest(func(ctx context.Context, c *netcheck.Client, dm *tailcfg.DERPMap, preferredDERP tailcfg.DERPRegionID, setCaptivePortal func(bool)) (done <-chan struct{}, stop func()) {
		return syncs.ClosedChan(), func() {}
	})
	os.Exit(m.Run())
}

func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestSanitizeRelPath(t *testing.T) {
	bad := []string{"", "/etc/passwd", "../x", "a/../../x", "a//b", "./a", "a/.."}
	for _, p := range bad {
		if got, err := sanitizeRelPath(p); err == nil {
			t.Errorf("sanitizeRelPath(%q) = %q, want error", p, got)
		}
	}
	good := map[string]string{
		"a.txt":         "a.txt",
		`dir\sub\f`:     "dir/sub/f",
		"what?.txt":     "what_.txt",
		"CON.txt":       "_CON.txt",
		"trailing. ":    "trailing",
		"Ordner/ä ö.md": "Ordner/ä ö.md",
	}
	for in, want := range good {
		got, err := sanitizeRelPath(in)
		if err != nil || got != want {
			t.Errorf("sanitizeRelPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestParseLink(t *testing.T) {
	addr := "tcomFwWCCcjS5nKNqAod034nWoJZW0LZqDhhC8U_dKdnDRYQ8uNGFpGQEu"
	for _, in := range []string{
		addr,
		"katzensprung://get/" + addr,
		"katzensprung://get/" + addr + "/",
		"KATZENSPRUNG://get/" + addr,
		" <katzensprung://get/" + addr + "> ",
	} {
		got, err := parseLink(in)
		if err != nil || string(got) != addr {
			t.Errorf("parseLink(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "https://example.com/" + addr, "katzensprung://get/nope"} {
		if _, err := parseLink(in); err == nil {
			t.Errorf("parseLink(%q): want error", in)
		}
	}
}

// TestEndToEnd shares a file and a folder over a real tailcat tunnel,
// bootstrapped through a local DERP relay, and receives them twice: once to an
// empty folder, once into the same folder to check nothing is overwritten.
func TestEndToEnd(t *testing.T) {
	dm := integration.RunDERPAndSTUN(t, logger.Discard, "127.0.0.1")
	derpRegionOverride = dm.Regions[1]
	t.Cleanup(func() { derpRegionOverride = nil })

	src := t.TempDir()
	big := make([]byte, 3<<20+17)
	rand.Read(big)
	must(t, os.WriteFile(filepath.Join(src, "foto.jpg"), big, 0o644))
	must(t, os.MkdirAll(filepath.Join(src, "Urlaub", "Tag 1"), 0o755))
	must(t, os.WriteFile(filepath.Join(src, "Urlaub", "notizen.txt"), []byte("hallo"), 0o644))
	must(t, os.WriteFile(filepath.Join(src, "Urlaub", "Tag 1", "leer.bin"), nil, 0o644))

	dst := t.TempDir()
	sender := NewApp()
	recv := NewApp()
	recv.settings.DownloadDir = dst

	sv, err := sender.CreateShare([]string{filepath.Join(src, "foto.jpg"), filepath.Join(src, "Urlaub")}, false)
	must(t, err)
	if sv.FileCount != 3 || sv.Total != int64(len(big)+5) {
		t.Fatalf("share = %+v", sv)
	}
	var link string
	waitFor(t, "share link", 60*time.Second, func() bool {
		st := sender.GetState()
		if st.Shares[0].Status == "error" {
			t.Fatalf("share error: %s", st.Shares[0].Error)
		}
		link = st.Shares[0].Link
		return link != ""
	})
	t.Logf("link: %s", link)

	for round := 1; round <= 2; round++ {
		id, err := recv.OpenLink(link)
		must(t, err)
		tr := func() TransferView {
			for _, x := range recv.GetState().Transfers {
				if x.ID == id {
					return x
				}
			}
			t.Fatal("transfer vanished")
			return TransferView{}
		}
		waitFor(t, "manifest", 60*time.Second, func() bool {
			v := tr()
			if v.Status == "error" {
				t.Fatalf("receive error: %s", v.Error)
			}
			return v.Status == "awaiting"
		})
		if v := tr(); v.FileCount != 3 || v.Title != "foto.jpg und Urlaub" {
			t.Fatalf("awaiting transfer = %+v", v)
		}
		must(t, recv.AcceptTransfer(id))
		waitFor(t, "transfer done", 60*time.Second, func() bool {
			v := tr()
			if v.Status == "error" {
				t.Fatalf("receive error: %s", v.Error)
			}
			return v.Status == "done"
		})
		t.Logf("round %d: route=%s (%s)", round, tr().Route, tr().RouteInfo)
	}

	check := func(rel string, want []byte) {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Errorf("read %s: %v", rel, err)
			return
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: content mismatch (%d vs %d bytes)", rel, len(got), len(want))
		}
	}
	check("foto.jpg", big)
	check("foto (2).jpg", big)
	check("Urlaub/notizen.txt", []byte("hallo"))
	check("Urlaub (2)/notizen.txt", []byte("hallo"))
	check("Urlaub/Tag 1/leer.bin", nil)

	waitFor(t, "sender done", 10*time.Second, func() bool {
		return sender.GetState().Shares[0].Downloads == 2
	})

	// Declining writes nothing and the sender sees it.
	id, err := recv.OpenLink(link)
	must(t, err)
	waitFor(t, "manifest", 60*time.Second, func() bool {
		for _, x := range recv.GetState().Transfers {
			if x.ID == id {
				return x.Status == "awaiting"
			}
		}
		return false
	})
	must(t, recv.DeclineTransfer(id))
	waitFor(t, "sender sees decline", 20*time.Second, func() bool {
		for _, x := range sender.GetState().Transfers {
			if x.Status == "declined" {
				return true
			}
		}
		return false
	})

	// Stopping the share makes the link stop working.
	must(t, sender.StopShare(sv.ID))
	id, err = recv.OpenLink(link)
	must(t, err)
	waitFor(t, "dial failure", 90*time.Second, func() bool {
		for _, x := range recv.GetState().Transfers {
			if x.ID == id {
				return x.Status == "error"
			}
		}
		return false
	})
	entries, _ := os.ReadDir(dst)
	if len(entries) != 4 {
		t.Errorf("download dir has %d entries, want 4", len(entries))
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
