package workflow

import (
	"testing"
	"time"
)

func TestFollowUpDecision(t *testing.T) {
	now := time.Date(2026, time.September, 24, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		metadata map[string]any
		want     bool
	}{
		{
			name: "unsigned order due in three days",
			metadata: map[string]any{
				"deadline":            now.Add(72 * time.Hour).Format(time.RFC3339),
				"signed_delivered_at": "",
			},
			want: true,
		},
		{
			name: "signed copy already delivered",
			metadata: map[string]any{
				"deadline":            now.Add(24 * time.Hour).Format(time.RFC3339),
				"signed_delivered_at": now.Add(-24 * time.Hour).Format(time.RFC3339),
			},
			want: false,
		},
		{
			name: "deadline outside follow-up window",
			metadata: map[string]any{
				"deadline":            now.Add(10 * 24 * time.Hour).Format(time.RFC3339),
				"signed_delivered_at": "",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := followUpDecision(now, tt.metadata)
			if got != tt.want {
				t.Fatalf("followUpDecision() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitTextKeepsEveryWord(t *testing.T) {
	got := splitText("alpha beta gamma delta", 12)
	if len(got) != 2 || got[0] != "alpha beta" || got[1] != "gamma delta" {
		t.Fatalf("splitText() = %#v", got)
	}
}
