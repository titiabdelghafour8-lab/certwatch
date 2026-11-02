package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/titiabdelghafour8-lab/certwatch/internal/config"
	"github.com/titiabdelghafour8-lab/certwatch/internal/model"
	"github.com/titiabdelghafour8-lab/certwatch/internal/version"
)

// Dispatcher sends messages over every supported channel kind.
type Dispatcher struct {
	HTTP *http.Client
	SMTP config.SMTPConfig
}

// EmailEnabled reports whether email delivery is configured.
func (d *Dispatcher) EmailEnabled() bool { return d.SMTP.Enabled() }

// Send delivers msg to ch.
func (d *Dispatcher) Send(ctx context.Context, ch model.Channel, msg Message) error {
	switch ch.Kind {
	case model.ChannelSlack:
		return d.postJSON(ctx, ch.Target, map[string]string{"text": msg.Text})
	case model.ChannelDiscord:
		return d.postJSON(ctx, ch.Target, map[string]string{"content": msg.Text})
	case model.ChannelWebhook:
		return d.postJSON(ctx, ch.Target, msg)
	case model.ChannelEmail:
		if !d.EmailEnabled() {
			return errors.New("email delivery is not configured on this server")
		}
		return d.SendEmail(ctx, ch.Target, msg.Subject, msg.Text+"\n\n-- \ncertwatch\n")
	default:
		return fmt.Errorf("unsupported channel kind %q", ch.Kind)
	}
}

func (d *Dispatcher) postJSON(ctx context.Context, url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	client := d.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webhook returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

// SendEmail sends a plain-text email. Port 465 uses implicit TLS; other
// ports upgrade with STARTTLS when the server offers it.
func (d *Dispatcher) SendEmail(ctx context.Context, to, subject, body string) error {
	cfg := d.SMTP
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return fmt.Errorf("invalid CERTWATCH_SMTP_FROM: %w", err)
	}
	rcpt, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	if cfg.Port == 465 {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp connect: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer c.Close()

	if cfg.Port != 465 {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		}
	}
	if cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err := c.Rcpt(rcpt.Address); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	clean := strings.NewReplacer("\r", " ", "\n", " ")
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from.String())
	fmt.Fprintf(&b, "To: %s\r\n", rcpt.String())
	fmt.Fprintf(&b, "Subject: %s\r\n", mimeEncode(clean.Replace(subject)))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return c.Quit()
}

// mimeEncode applies RFC 2047 encoding to header values with non-ASCII text.
func mimeEncode(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
		}
	}
	return s
}
