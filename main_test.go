package main

import (
	"testing"
	"time"
)

func TestResolveTime(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, result string)
	}{
		{
			name:  "empty uses default offset",
			input: "",
			check: func(t *testing.T, result string) {
				parsed, _ := time.Parse(time.RFC3339, result)
				diff := now.Sub(parsed)
				if diff < 23*time.Hour || diff > 25*time.Hour {
					t.Errorf("expected ~24h ago, got %v ago", diff)
				}
			},
		},
		{
			name:  "days shorthand",
			input: "7d",
			check: func(t *testing.T, result string) {
				parsed, _ := time.Parse(time.RFC3339, result)
				diff := now.Sub(parsed)
				expected := 7 * 24 * time.Hour
				if diff < expected-time.Minute || diff > expected+time.Minute {
					t.Errorf("expected ~7d ago, got %v ago", diff)
				}
			},
		},
		{
			name:  "hours shorthand",
			input: "12h",
			check: func(t *testing.T, result string) {
				parsed, _ := time.Parse(time.RFC3339, result)
				diff := now.Sub(parsed)
				expected := 12 * time.Hour
				if diff < expected-time.Minute || diff > expected+time.Minute {
					t.Errorf("expected ~12h ago, got %v ago", diff)
				}
			},
		},
		{
			name:  "minutes shorthand",
			input: "45m",
			check: func(t *testing.T, result string) {
				parsed, _ := time.Parse(time.RFC3339, result)
				diff := now.Sub(parsed)
				expected := 45 * time.Minute
				if diff < expected-time.Minute || diff > expected+time.Minute {
					t.Errorf("expected ~45m ago, got %v ago", diff)
				}
			},
		},
		{
			name:  "RFC3339 passthrough",
			input: "2025-01-15T10:00:00Z",
			check: func(t *testing.T, result string) {
				if result != "2025-01-15T10:00:00Z" {
					t.Errorf("expected passthrough, got %s", result)
				}
			},
		},
		{
			name:    "invalid input",
			input:   "notadate",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolveTime(tt.input, -24*time.Hour)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}
