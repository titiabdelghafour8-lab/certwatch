package billing

import (
	"errors"
	"testing"
	"time"
)

func TestVerifySignature(t *testing.T) {
	payload := []byte(`{"id":"evt_1","type":"checkout.session.completed"}`)
	now := time.Now()
	header := Sign(payload, "whsec_test", now)

	if err := VerifySignature(payload, header, "whsec_test", now, 5*time.Minute); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := VerifySignature(payload, header, "whsec_other", now, 5*time.Minute); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("wrong secret: got %v", err)
	}
	if err := VerifySignature([]byte(`{}`), header, "whsec_test", now, 5*time.Minute); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("tampered payload: got %v", err)
	}
	if err := VerifySignature(payload, header, "whsec_test", now.Add(time.Hour), 5*time.Minute); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("replayed event: got %v", err)
	}
	if err := VerifySignature(payload, "garbage", "whsec_test", now, 5*time.Minute); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("malformed header: got %v", err)
	}
}

func TestSubscriptionActive(t *testing.T) {
	for status, want := range map[string]bool{"active": true, "trialing": true, "past_due": true, "canceled": false, "unpaid": false} {
		if got := (Subscription{Status: status}).Active(); got != want {
			t.Errorf("Active(%s) = %v", status, got)
		}
	}
}
