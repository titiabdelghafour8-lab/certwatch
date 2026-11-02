// Package notify delivers alerts to notification channels.
package notify

import (
	"fmt"
	"time"

	"github.com/titiabdelghafour8-lab/certwatch/internal/model"
)

// Event identifies why a message is sent. It is part of the webhook payload
// contract, so values must stay stable.
type Event string

const (
	EventExpiring    Event = "expiring"
	EventExpired     Event = "expired"
	EventCheckFailed Event = "check_failed"
	EventTest        Event = "test"
)

type Message struct {
	Event    Event          `json:"event"`
	Subject  string         `json:"subject"`
	Text     string         `json:"text"`
	Monitor  *model.Monitor `json:"monitor,omitempty"`
	DaysLeft *int           `json:"days_left,omitempty"`
	SentAt   time.Time      `json:"sent_at"`
}

// ExpiryMessage builds the alert for a monitor approaching or past expiry.
func ExpiryMessage(m model.Monitor, daysLeft int) Message {
	what := m.Describe()
	date := ""
	if m.ExpiresAt != nil {
		date = m.ExpiresAt.UTC().Format("2006-01-02")
	}
	msg := Message{Event: EventExpiring, Monitor: &m, DaysLeft: &daysLeft, SentAt: time.Now().UTC()}
	switch {
	case daysLeft < 0:
		msg.Event = EventExpired
		msg.Subject = fmt.Sprintf("[certwatch] EXPIRED: %s", m.Target)
		msg.Text = fmt.Sprintf("🚨 %s EXPIRED on %s (%d days ago).", what, date, -daysLeft)
	case daysLeft == 0:
		msg.Subject = fmt.Sprintf("[certwatch] %s expires today", m.Target)
		msg.Text = fmt.Sprintf("🚨 %s expires TODAY (%s).", what, date)
	default:
		icon := "⚠️"
		if daysLeft <= 7 {
			icon = "🚨"
		}
		msg.Subject = fmt.Sprintf("[certwatch] %s expires in %d days", m.Target, daysLeft)
		msg.Text = fmt.Sprintf("%s %s expires in %d days (%s).", icon, what, daysLeft, date)
	}
	if m.Label != "" {
		msg.Text += " Label: " + m.Label + "."
	}
	return msg
}

// FailureMessage builds the alert for a monitor that keeps failing to check.
func FailureMessage(m model.Monitor, cause error) Message {
	return Message{
		Event:   EventCheckFailed,
		Subject: fmt.Sprintf("[certwatch] cannot check %s", m.Target),
		Text:    fmt.Sprintf("❌ %s could not be checked %d times in a row: %v", m.Describe(), m.ConsecutiveFailures, cause),
		Monitor: &m,
		SentAt:  time.Now().UTC(),
	}
}

// TestMessage is sent by "certwatch channels test".
func TestMessage() Message {
	return Message{
		Event:   EventTest,
		Subject: "[certwatch] test notification",
		Text:    "✅ This is a test notification from certwatch. Your channel is set up correctly.",
		SentAt:  time.Now().UTC(),
	}
}
