package cli

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/titiabdelghafour8/certwatch/internal/alerting"
	"github.com/titiabdelghafour8/certwatch/internal/checker"
	"github.com/titiabdelghafour8/certwatch/internal/model"
)

type localResult struct {
	Input     string            `json:"input"`
	Kind      model.MonitorKind `json:"kind"`
	Target    string            `json:"target"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
	DaysLeft  *int              `json:"days_left,omitempty"`
	Status    model.Status      `json:"status"`
	Issuer    string            `json:"issuer,omitempty"`
	Error     string            `json:"error,omitempty"`
}

func (a *app) checkCmd() *cobra.Command {
	var (
		port      int
		tlsOnly   bool
		domOnly   bool
		failUnder int
		timeout   time.Duration
	)
	cmd := &cobra.Command{
		Use:   "check <host>...",
		Short: "Check certificate and domain expiry right now, locally",
		Long: `Check TLS certificate and domain registration expiry directly from this
machine. No account is needed, and it works against internal hosts too.

Use --fail-under in CI or cron to exit non-zero when anything expires soon.`,
		Example: `  certwatch check example.com github.com
  certwatch check internal.corp:8443 --tls-only
  certwatch check example.com --fail-under 14   # exit 1 if < 14 days left`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if tlsOnly && domOnly {
				return fmt.Errorf("--tls-only and --domain-only are mutually exclusive")
			}
			tlsC := &checker.TLSChecker{Timeout: timeout, AllowPrivate: true}
			domC := &checker.DomainChecker{}

			type job struct {
				input  string
				kind   model.MonitorKind
				target string
				err    error
			}
			var jobs []job
			for _, in := range args {
				if !domOnly {
					t, err := checker.NormalizeTLSTarget(in, port)
					jobs = append(jobs, job{in, model.KindTLS, t, err})
				}
				if !tlsOnly {
					d, err := checker.NormalizeDomain(in)
					if err == nil || domOnly {
						jobs = append(jobs, job{in, model.KindDomain, d, err})
					}
				}
			}

			results := make([]localResult, len(jobs))
			var wg sync.WaitGroup
			now := time.Now()
			for i, j := range jobs {
				wg.Add(1)
				go func(i int, j job) {
					defer wg.Done()
					r := localResult{Input: j.input, Kind: j.kind, Target: j.target}
					if j.err != nil {
						r.Status, r.Error = model.StatusError, j.err.Error()
						results[i] = r
						return
					}
					ctx, cancel := context.WithTimeout(cmd.Context(), timeout+10*time.Second)
					defer cancel()
					var c checker.Checker = tlsC
					if j.kind == model.KindDomain {
						c = domC
					}
					res, err := c.Check(ctx, j.target)
					if err != nil {
						r.Status, r.Error = model.StatusError, err.Error()
						results[i] = r
						return
					}
					days := model.DaysUntil(res.ExpiresAt, now)
					r.ExpiresAt, r.DaysLeft, r.Issuer = &res.ExpiresAt, &days, res.Issuer
					r.Status = alerting.StatusFor(days, model.DefaultWarnDays)
					if res.VerifyError != "" {
						r.Error = "certificate does not validate: " + res.VerifyError
					}
					results[i] = r
				}(i, j)
			}
			wg.Wait()

			if a.jsonOut {
				if err := printJSON(a.out, results); err != nil {
					return err
				}
			} else {
				color := colorEnabled(a.out)
				t := newTable(a.out)
				fmt.Fprintln(t, "TARGET\tKIND\tEXPIRES\tDAYS\tSTATUS\tISSUER\tNOTE")
				for _, r := range results {
					fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Target, r.Kind, fmtDate(r.ExpiresAt),
						fmtDays(r.DaysLeft), colorStatus(r.Status, color), truncate(r.Issuer, 28), truncate(r.Error, 70))
				}
				_ = t.Flush()
			}

			failed, low := 0, 0
			for _, r := range results {
				if r.Status == model.StatusError {
					failed++
				}
				if failUnder > 0 && r.DaysLeft != nil && *r.DaysLeft < failUnder {
					low++
				}
			}
			switch {
			case low > 0:
				return &ExitError{Code: 1, Err: fmt.Errorf("%d item(s) expire in fewer than %d days", low, failUnder)}
			case failed > 0:
				return &ExitError{Code: 2, Err: fmt.Errorf("%d check(s) failed", failed)}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVarP(&port, "port", "p", 443, "TLS port when the host has none")
	f.BoolVar(&tlsOnly, "tls-only", false, "only check TLS certificates")
	f.BoolVar(&domOnly, "domain-only", false, "only check domain registrations")
	f.IntVar(&failUnder, "fail-under", 0, "exit with status 1 if anything expires in fewer than N days")
	f.DurationVar(&timeout, "timeout", 10*time.Second, "TLS connection timeout")
	return cmd
}
