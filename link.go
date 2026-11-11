package main

import (
	"errors"
	"net/url"
	"strings"

	"github.com/tailscale/tailcat"
)

const linkScheme = "katzensprung"

// makeLink turns a tailcat address into a share link. The address goes in
// the path, not the host: tailcat addresses are case-sensitive and URL
// handlers are free to lowercase hostnames.
func makeLink(addr tailcat.Addr) string {
	return linkScheme + "://get/" + string(addr)
}

// parseLink accepts a share link (also with a trailing slash or query, as
// some browsers and chat apps add) or a bare tailcat address.
func parseLink(s string) (tailcat.Addr, error) {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "<>\"'")
	if s == "" {
		return "", errors.New("no link given")
	}
	addr := s
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || !strings.EqualFold(u.Scheme, linkScheme) {
			return "", errors.New("that is not a Katzensprung link")
		}
		p := strings.Trim(u.Path, "/")
		if strings.EqualFold(u.Host, "get") {
			addr = p
		} else {
			// katzensprung:///get/<addr> or katzensprung://<addr>
			addr = strings.TrimPrefix(p, "get/")
			if addr == "" {
				addr = u.Host
			}
		}
	}
	addr = strings.Trim(addr, "/ ")
	if _, err := tailcat.ParseAddr(tailcat.Addr(addr)); err != nil {
		return "", errors.New("the link is invalid or incomplete")
	}
	return tailcat.Addr(addr), nil
}

// linkFromArgs finds a share link in command-line arguments, as passed by
// the OS when a link is clicked.
func linkFromArgs(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(strings.ToLower(a), linkScheme+":") {
			return a
		}
	}
	return ""
}
