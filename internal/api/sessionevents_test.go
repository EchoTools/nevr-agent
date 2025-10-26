package api

import (
	"testing"

	"github.com/gofrs/uuid/v5"
)

func TestMatchID_IsValid(t *testing.T) {
	tests := []struct {
		name string
		id   MatchID
		want bool
	}{
		{
			name: "valid match ID",
			id: MatchID{
				UUID: uuid.Must(uuid.NewV4()),
				Node: "node1",
			},
			want: true,
		},
		{
			name: "empty match ID",
			id:   MatchID{},
			want: false,
		},
		{
			name: "nil UUID",
			id: MatchID{
				UUID: uuid.Nil,
				Node: "node1",
			},
			want: false,
		},
		{
			name: "empty node",
			id: MatchID{
				UUID: uuid.Must(uuid.NewV4()),
				Node: "",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.id.IsValid(); got != tt.want {
				t.Errorf("MatchID.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchIDFromString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid match ID string",
			input:   "550e8400-e29b-41d4-a716-446655440000.node1",
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: false, // Should return empty MatchID without error
		},
		{
			name:    "invalid format - no dot",
			input:   "550e8400-e29b-41d4-a716-446655440000",
			wantErr: true,
		},
		{
			name:    "invalid format - empty node",
			input:   "550e8400-e29b-41d4-a716-446655440000.",
			wantErr: true,
		},
		{
			name:    "invalid UUID",
			input:   "invalid-uuid.node1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MatchIDFromString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("MatchIDFromString() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && tt.input != "" {
				if !result.IsValid() {
					t.Errorf("MatchIDFromString() returned invalid MatchID for valid input: %v", tt.input)
				}
				if result.String() != tt.input {
					t.Errorf("MatchIDFromString() round-trip failed: got %v, want %v", result.String(), tt.input)
				}
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("DefaultConfig() returned nil")
	}

	if err := config.Validate(); err != nil {
		t.Errorf("DefaultConfig() returned invalid configuration: %v", err)
	}

	// Check that required fields are set
	if config.MongoURI == "" {
		t.Error("DefaultConfig() should set MongoURI")
	}
	if config.DatabaseName == "" {
		t.Error("DefaultConfig() should set DatabaseName")
	}
	if config.CollectionName == "" {
		t.Error("DefaultConfig() should set CollectionName")
	}
	if config.ServerAddress == "" {
		t.Error("DefaultConfig() should set ServerAddress")
	}
}
