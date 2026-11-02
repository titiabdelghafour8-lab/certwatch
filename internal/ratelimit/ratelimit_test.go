package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := New(time.Hour, 2)
	for i := 0; i < 2; i++ {
		if !l.Allow("a") {
			t.Fatalf("request %d within burst should be allowed", i+1)
		}
	}
	if l.Allow("a") {
		t.Fatal("third request should be limited")
	}
	if !l.Allow("b") {
		t.Fatal("keys must be independent")
	}
}
