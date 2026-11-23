package main

import (
	_ "embed"
	"strings"
)

// version.txt says "dev" in the repository; release builds overwrite it
// with the release tag before building.
//
//go:embed version.txt
var versionTxt string

var appVersion = strings.TrimSpace(versionTxt)
