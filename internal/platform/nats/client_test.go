package platformnats

import (
	"testing"
)

func TestClientConfig_Defaults(t *testing.T) {
	t.Parallel()

	cfg := ClientConfig{}
	if cfg.URL != "" {
		t.Errorf("URL = %v, want empty (defaults applied in NewClient)", cfg.URL)
	}
}

func TestNewClient_InvalidURL(t *testing.T) {
	t.Parallel()

	_, err := NewClient(ClientConfig{
		URL:  "nats://invalid-host-that-does-not-exist:4222",
		Name: "test",
	})
	if err == nil {
		t.Fatal("expected error for invalid NATS URL, got nil")
	}
}
