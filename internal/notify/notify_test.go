package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/titiabdelghafour8/certwatch/internal/model"
)

func TestSendWebhooks(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()

	d := &Dispatcher{HTTP: srv.Client()}
	exp := time.Now().Add(5 * 24 * time.Hour)
	m := model.Monitor{Kind: model.KindTLS, Target: "example.com:443", ExpiresAt: &exp}
	msg := ExpiryMessage(m, 5)

	if err := d.Send(context.Background(), model.Channel{Kind: model.ChannelSlack, Target: srv.URL}, msg); err != nil {
		t.Fatal(err)
	}
	if text, _ := got["text"].(string); !strings.Contains(text, "expires in 5 days") {
		t.Errorf("slack payload text = %q", text)
	}

	if err := d.Send(context.Background(), model.Channel{Kind: model.ChannelDiscord, Target: srv.URL}, msg); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["content"]; !ok {
		t.Errorf("discord payload missing content: %v", got)
	}

	if err := d.Send(context.Background(), model.Channel{Kind: model.ChannelWebhook, Target: srv.URL}, msg); err != nil {
		t.Fatal(err)
	}
	if got["event"] != string(EventExpiring) || got["days_left"] != float64(5) {
		t.Errorf("webhook payload = %v", got)
	}
}

func TestSendWebhookError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusGone)
	}))
	defer srv.Close()
	d := &Dispatcher{HTTP: srv.Client()}
	err := d.Send(context.Background(), model.Channel{Kind: model.ChannelWebhook, Target: srv.URL}, TestMessage())
	if err == nil || !strings.Contains(err.Error(), "410") {
		t.Fatalf("expected HTTP 410 error, got %v", err)
	}
}

func TestExpiryMessage(t *testing.T) {
	m := model.Monitor{Kind: model.KindDomain, Target: "example.com"}
	if msg := ExpiryMessage(m, -2); msg.Event != EventExpired || !strings.Contains(msg.Text, "2 days ago") {
		t.Errorf("expired message = %+v", msg)
	}
	if msg := ExpiryMessage(m, 0); !strings.Contains(msg.Text, "TODAY") {
		t.Errorf("today message = %+v", msg)
	}
}

func TestEmailDisabled(t *testing.T) {
	d := &Dispatcher{}
	err := d.Send(context.Background(), model.Channel{Kind: model.ChannelEmail, Target: "a@b.c"}, TestMessage())
	if err == nil {
		t.Fatal("expected error when SMTP is not configured")
	}
}
