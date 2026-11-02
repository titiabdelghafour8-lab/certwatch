// Package version holds build metadata injected at link time via -ldflags.
package version

import "fmt"

// These variables are overridden at build time, e.g.
//
//	-X github.com/titiabdelghafour8-lab/certwatch/internal/version.Version=v1.2.3
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"

	// DefaultAPIURL is the API endpoint the CLI talks to when nothing else is
	// configured. Release builds point it at the hosted service.
	DefaultAPIURL = "http://localhost:8080"
)

// String returns a human readable version line.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}

// UserAgent is sent with every outbound HTTP request.
func UserAgent() string {
	return "certwatch/" + Version + " (+https://github.com/titiabdelghafour8-lab/certwatch)"
}
