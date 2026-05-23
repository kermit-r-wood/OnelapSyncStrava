package main

import (
	"testing"
	"time"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 30, 0, 0, time.Local)

	tests := []struct {
		name  string
		input string
		want  time.Time
	}{
		{"absolute date", "2026-05-01", time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local)},
		{"absolute datetime", "2026-05-01 09:30:15", time.Date(2026, 5, 1, 9, 30, 15, 0, time.Local)},
		{"relative days", "7d", time.Date(2026, 5, 17, 0, 0, 0, 0, time.Local)},
		{"relative weeks", "2w", time.Date(2026, 5, 10, 0, 0, 0, 0, time.Local)},
		{"relative months", "6m", time.Date(2025, 11, 24, 0, 0, 0, 0, time.Local)},
		{"relative years", "1y", time.Date(2025, 5, 24, 0, 0, 0, 0, time.Local)},
		{"zero days is today at midnight", "0d", time.Date(2026, 5, 24, 0, 0, 0, 0, time.Local)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSince(tc.input, now)
			if err != nil {
				t.Fatalf("parseSince(%q) unexpected error: %v", tc.input, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("parseSince(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseSinceErrors(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.Local)
	invalid := []string{
		"",
		"abc",
		"7x",        // unknown unit
		"-1d",       // negative not allowed by regex
		"1.5d",      // non-integer
		"d",         // missing number
		"2026/05/01", // wrong separator
	}
	for _, in := range invalid {
		name := in
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			if _, err := parseSince(in, now); err == nil {
				t.Errorf("parseSince(%q) expected error, got nil", in)
			}
		})
	}
}
