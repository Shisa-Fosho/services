package sharednats

import (
	"testing"

	"github.com/nats-io/nats.go"
)

func TestEnsureKeyValue_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *nats.KeyValueConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
		{
			name:    "empty bucket name",
			cfg:     &nats.KeyValueConfig{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// EnsureKeyValue requires a real JetStream connection, but
			// validation errors return before any connection use.
			c := &Client{}
			_, err := c.EnsureKeyValue(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureKeyValue() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
