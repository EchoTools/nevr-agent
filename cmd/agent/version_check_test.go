package main

import (
	"testing"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		current  string
		latest   string
		expected bool
	}{
		// Basic cases
		{"1.0.0", "1.0.1", true},
		{"1.0.0", "1.1.0", true},
		{"1.0.0", "2.0.0", true},
		{"1.0.1", "1.0.0", false},
		{"1.1.0", "1.0.0", false},
		{"2.0.0", "1.0.0", false},
		{"1.0.0", "1.0.0", false},

		// With v prefix
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "1.0.1", true},
		{"1.0.0", "v1.0.1", true},

		// Dev version
		{"dev", "1.0.0", true},
		{"dev", "0.0.1", true},
		{"", "1.0.0", true},

		// Partial versions
		{"1.0", "1.0.1", true},
		{"1", "1.0.1", true},
		{"1.0.0", "1.1", true},

		// Pre-release versions (numeric extraction)
		{"1.0.0-beta", "1.0.0", false},
		{"1.0.0", "1.0.1-beta", true},
	}

	for _, tt := range tests {
		t.Run(tt.current+"_vs_"+tt.latest, func(t *testing.T) {
			result := isNewerVersion(tt.current, tt.latest)
			if result != tt.expected {
				t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tt.current, tt.latest, result, tt.expected)
			}
		})
	}
}

func TestExtractNumeric(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"1", 1},
		{"12", 12},
		{"123", 123},
		{"1-beta", 1},
		{"12-rc1", 12},
		{"0", 0},
		{"", 0},
		{"beta", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractNumeric(tt.input)
			if result != tt.expected {
				t.Errorf("extractNumeric(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}
