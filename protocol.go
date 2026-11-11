package main

// Wire protocol between a sender (tailcat server) and a receiver (tailcat
// client), spoken over one TCP connection inside the WireGuard tunnel:
//
//	sender   -> receiver: manifest (one JSON line)
//	receiver -> sender:   reply    (one JSON line: accept or decline)
//	sender   -> receiver: the bytes of every file in manifest order,
//	                      exactly Size bytes each, back to back
//	receiver -> sender:   done     (one JSON line) once everything is on disk
//
// Integrity and confidentiality come from WireGuard; the protocol only has
// to frame the files.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode"
)

const (
	protocolVersion = 1
	transferPort    = 4747
	maxLineBytes    = 4 << 20 // a manifest of tens of thousands of files fits
)

type manifest struct {
	Version int            `json:"v"`
	Title   string         `json:"title"`
	Files   []manifestFile `json:"files"`
	Total   int64          `json:"total"`
}

type manifestFile struct {
	Path string `json:"path"` // slash-separated, relative
	Size int64  `json:"size"`
}

type reply struct {
	Accept bool `json:"accept"`
}

type doneMsg struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func writeLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

func readLine(r *bufio.Reader, v any) error {
	var line []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return err
		}
		line = append(line, chunk...)
		if len(line) > maxLineBytes {
			return errors.New("protokollzeile zu lang")
		}
		if !isPrefix {
			break
		}
	}
	return json.Unmarshal(line, v)
}

// validate checks a manifest received from an untrusted sender.
func (m *manifest) validate() error {
	if m.Version != protocolVersion {
		return fmt.Errorf("inkompatible Version des Absenders (%d)", m.Version)
	}
	if len(m.Files) == 0 {
		return errors.New("die Freigabe enthält keine Dateien")
	}
	var total int64
	seen := make(map[string]bool, len(m.Files))
	for i := range m.Files {
		f := &m.Files[i]
		if f.Size < 0 {
			return errors.New("ungültige Dateigröße")
		}
		p, err := sanitizeRelPath(f.Path)
		if err != nil {
			return fmt.Errorf("ungültiger Dateiname %q: %w", f.Path, err)
		}
		key := strings.ToLower(p)
		if seen[key] {
			return fmt.Errorf("doppelter Dateiname %q", p)
		}
		seen[key] = true
		f.Path = p
		total += f.Size
	}
	if total != m.Total {
		return errors.New("Gesamtgröße passt nicht zu den Dateien")
	}
	m.Title = cleanTitle(m.Title)
	return nil
}

// sanitizeRelPath turns a sender-supplied path into a safe relative
// slash-separated path: no absolute paths, no "." or ".." components, no
// characters that are invalid in file names on Windows. Writing is
// additionally confined to the target directory with os.Root.
func sanitizeRelPath(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" || strings.HasPrefix(p, "/") {
		return "", errors.New("leer oder absolut")
	}
	parts := strings.Split(p, "/")
	if len(parts) > 64 {
		return "", errors.New("zu tief verschachtelt")
	}
	out := make([]string, 0, len(parts))
	for _, c := range parts {
		c = cleanComponent(c)
		if c == "" || c == "." || c == ".." {
			return "", errors.New("ungültiger Pfadbestandteil")
		}
		out = append(out, c)
	}
	return path.Join(out...), nil
}

var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

func cleanComponent(c string) string {
	c = strings.Map(func(r rune) rune {
		switch {
		case r < 32 || r == 127, strings.ContainsRune(`<>:"|?*`, r):
			return '_'
		}
		return r
	}, c)
	c = strings.TrimRight(c, " .")
	if c == "" {
		return ""
	}
	if len(c) > 255 {
		c = c[:255]
	}
	base := strings.ToUpper(strings.SplitN(c, ".", 2)[0])
	if windowsReserved[base] {
		c = "_" + c
	}
	return c
}

func cleanTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return strings.TrimSpace(s)
}
