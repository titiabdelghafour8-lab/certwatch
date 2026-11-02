package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/titiabdelghafour8/certwatch/internal/model"
)

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func newTable(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

// colorEnabled follows https://no-color.org and only colors terminals.
func colorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

const (
	ansiReset  = "\x1b[0m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
	ansiGreen  = "\x1b[32m"
	ansiGray   = "\x1b[90m"
)

func colorStatus(s model.Status, color bool) string {
	if !color {
		return string(s)
	}
	c := ansiGray
	switch s {
	case model.StatusOK:
		c = ansiGreen
	case model.StatusWarning:
		c = ansiYellow
	case model.StatusCritical, model.StatusExpired, model.StatusError:
		c = ansiRed
	}
	return c + string(s) + ansiReset
}

func fmtDate(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.UTC().Format("2006-01-02")
}

func fmtDays(d *int) string {
	if d == nil {
		return "-"
	}
	return strconv.Itoa(*d)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func printMonitors(w io.Writer, ms []model.Monitor) {
	color := colorEnabled(w)
	t := newTable(w)
	fmt.Fprintln(t, "ID\tKIND\tTARGET\tEXPIRES\tDAYS\tSTATUS\tISSUER\tNOTE")
	for _, m := range ms {
		status := colorStatus(m.Status, color)
		if m.Paused {
			status += " (paused)"
		}
		note := m.LastError
		if m.Label != "" && note == "" {
			note = m.Label
		}
		fmt.Fprintf(t, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", m.ID, m.Kind, m.Target, fmtDate(m.ExpiresAt),
			fmtDays(m.DaysLeft), status, truncate(m.Issuer, 28), truncate(note, 60))
	}
	_ = t.Flush()
}

// openBrowser tries to open url; failure is not an error since the URL is
// always printed as well.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func parseIntList(s string) ([]int, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []int
	for _, part := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("invalid number %q", part)
		}
		out = append(out, n)
	}
	return out, nil
}
