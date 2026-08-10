// Package buildinfo exposes the version details stamped into the binary at
// build time, falling back to whatever the Go toolchain embedded.
package buildinfo

import (
	"runtime"
	"runtime/debug"
	"sync"
)

// Overridden at build time with -ldflags "-X github.com/marcelblijleven/homewizard_exporter/internal/buildinfo.Version=..."
var (
	Version = ""
	Commit  = ""
	Date    = ""
)

var once sync.Once

// Info describes the running binary.
type Info struct {
	Version   string
	Commit    string
	Date      string
	GoVersion string
}

var info Info

// Get returns the build details, filling in any blanks from the module's
// embedded VCS stamps so that `go install`-ed binaries still report usefully.
func Get() Info {
	once.Do(func() {
		info = Info{Version: Version, Commit: Commit, Date: Date, GoVersion: runtime.Version()}

		bi, ok := debug.ReadBuildInfo()
		if !ok {
			return
		}
		if info.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.Version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.Date == "" {
					info.Date = s.Value
				}
			case "vcs.modified":
				if s.Value == "true" && info.Commit != "" {
					info.Commit += "-dirty"
				}
			}
		}
		if info.Version == "" {
			info.Version = "dev"
		}
	})
	return info
}

// String renders the build details on a single line.
func (i Info) String() string {
	s := "homewizard_exporter " + i.Version
	if i.Commit != "" {
		s += " (" + i.Commit + ")"
	}
	if i.Date != "" {
		s += " built " + i.Date
	}
	return s + " " + i.GoVersion
}
