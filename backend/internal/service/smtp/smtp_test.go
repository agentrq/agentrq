package smtp

import (
	"context"
	"testing"
)

func TestSmtp(t *testing.T) {
	t.Run("Disabled", func(t *testing.T) {
		s := New(Config{Enabled: false})
		err := s.Send(context.Background(), SendRequest{})
		if err == nil || err.Error() != "smtp service is disabled" {
			t.Errorf("expected disabled error, got %v", err)
		}
	})

	t.Run("NoHost", func(t *testing.T) {
		s := New(Config{Enabled: true, Host: ""})
		err := s.Send(context.Background(), SendRequest{})
		if err == nil || err.Error() != "smtp host is not configured" {
			t.Errorf("expected host not configured error, got %v", err)
		}
	})

	t.Run("SendMailFail", func(t *testing.T) {
		// This will fail because it'll try to reach localhost:25 which is likely down
		s := New(Config{Enabled: true, Host: "localhost", Port: 25})
		err := s.Send(context.Background(), SendRequest{To: []string{"a@b.com"}})
		if err == nil {
			t.Error("expected error for closed port, got nil")
		}
	})

	t.Run("TLSDialFail", func(t *testing.T) {
		s := New(Config{Enabled: true, Host: "localhost", Port: 465})
		err := s.Send(context.Background(), SendRequest{To: []string{"a@b.com"}})
		if err == nil {
			t.Error("expected error for closed TLS port, got nil")
		}
	})
}
