package mcp

import "testing"

func TestLoggedHeaderValueRedactsSensitiveHeaders(t *testing.T) {
	tests := []struct {
		name   string
		header string
		values []string
		want   string
	}{
		{
			name:   "authorization",
			header: "Authorization",
			values: []string{"Bearer secret-token"},
			want:   "[REDACTED]",
		},
		{
			name:   "cookie",
			header: "Cookie",
			values: []string{"at=session-token; theme=light"},
			want:   "[REDACTED]",
		},
		{
			name:   "api key",
			header: "X-Api-Key",
			values: []string{"secret-key"},
			want:   "[REDACTED]",
		},
		{
			name:   "safe header",
			header: "Mcp-Protocol-Version",
			values: []string{"2024-11-05"},
			want:   "2024-11-05",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loggedHeaderValue(tt.header, tt.values)
			if got != tt.want {
				t.Fatalf("loggedHeaderValue(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}
