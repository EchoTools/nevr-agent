package config

import (
	"testing"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		// Basic numbers (bytes)
		{"0", 0, false},
		{"100", 100, false},
		{"1000", 1000, false},

		// Empty string
		{"", 0, false},
		{"  ", 0, false},

		// Kilobytes
		{"1K", 1024, false},
		{"1k", 1024, false},
		{"1KB", 1024, false},
		{"1kb", 1024, false},
		{"1KiB", 1024, false},
		{"1000K", 1024 * 1000, false},
		{"1.5K", 1536, false},

		// Megabytes
		{"1M", 1024 * 1024, false},
		{"1m", 1024 * 1024, false},
		{"1MB", 1024 * 1024, false},
		{"500M", 500 * 1024 * 1024, false},
		{"1.5M", int64(1.5 * 1024 * 1024), false},

		// Gigabytes
		{"1G", 1024 * 1024 * 1024, false},
		{"1g", 1024 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"10G", 10 * 1024 * 1024 * 1024, false},
		{"6G", 6 * 1024 * 1024 * 1024, false},

		// Terabytes
		{"1T", 1024 * 1024 * 1024 * 1024, false},
		{"1t", 1024 * 1024 * 1024 * 1024, false},
		{"1TB", 1024 * 1024 * 1024 * 1024, false},

		// With spaces
		{"1 G", 1024 * 1024 * 1024, false},
		{" 500M ", 500 * 1024 * 1024, false},

		// Invalid formats
		{"abc", 0, true},
		{"1X", 0, true},
		{"-1G", 0, true},
		{"1.2.3G", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseByteSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseByteSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != tt.expected {
				t.Errorf("ParseByteSize(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatByteSize(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0B"},
		{100, "100B"},
		{1023, "1023B"},
		{1024, "1.0KiB"},
		{1536, "1.5KiB"},
		{1024 * 1024, "1.0MiB"},
		{1024 * 1024 * 1024, "1.0GiB"},
		{10 * 1024 * 1024 * 1024, "10.0GiB"},
		{1024 * 1024 * 1024 * 1024, "1.0TiB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatByteSize(tt.input)
			if result != tt.expected {
				t.Errorf("FormatByteSize(%d) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
