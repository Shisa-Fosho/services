package sharednats

import (
	"testing"
)

func TestStreamConfig_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     StreamConfig
		wantErr bool
	}{
		{
			name:    "empty name",
			cfg:     StreamConfig{Subjects: []string{"test.>"}},
			wantErr: true,
		},
		{
			name:    "empty subjects",
			cfg:     StreamConfig{Name: "TEST"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// EnsureStream requires a real JetStream connection,
			// but validation errors are returned before any connection use.
			c := &Client{}
			_, err := c.EnsureStream(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureStream() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEnsureConsumer_Validation(t *testing.T) {
	t.Parallel()

	c := &Client{}

	_, err := c.EnsureConsumer("", nil)
	if err == nil {
		t.Error("expected error for empty stream name, got nil")
	}

	_, err = c.EnsureConsumer("TEST", nil)
	if err == nil {
		t.Error("expected error for nil config, got nil")
	}
}
