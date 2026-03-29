package agent

import (
	"strings"
	"testing"
)

func TestSanitizeLog(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantMasked  []string // substrings that must NOT appear in the output
		wantPresent []string // substrings that must still appear in the output
	}{
		{
			name:        "sk- key is masked",
			input:       "error: invalid key sk-abc1234567890XYZ",
			wantMasked:  []string{"sk-abc1234567890XYZ"},
			wantPresent: []string{"sk-***", "error: invalid key"},
		},
		{
			name:        "AIza key is masked",
			input:       "using api key AIzaSyD-9tSrke72I6e49zkreH8HsGzABCD1234",
			wantMasked:  []string{"AIzaSyD-9tSrke72I6e49zkreH8HsGzABCD1234"},
			wantPresent: []string{"AIza***"},
		},
		{
			name:        "Bearer token is masked",
			input:       "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			wantMasked:  []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
			wantPresent: []string{"Bearer ***"},
		},
		{
			name:        "bearer token case-insensitive",
			input:       "header: bearer supersecrettoken12345",
			wantMasked:  []string{"supersecrettoken12345"},
			wantPresent: []string{"bearer ***"},
		},
		{
			name:        "key= query parameter is masked",
			input:       "GET /api?key=mysecretapikey123 HTTP/1.1",
			wantMasked:  []string{"mysecretapikey123"},
			wantPresent: []string{"key=***"},
		},
		{
			name:        "api_key= is masked",
			input:       "request failed: api_key=supersecret123",
			wantMasked:  []string{"supersecret123"},
			wantPresent: []string{"api_key=***"},
		},
		{
			name:        "token= is masked",
			input:       "token=abcdefghij refresh failed",
			wantMasked:  []string{"abcdefghij"},
			wantPresent: []string{"token=***"},
		},
		{
			name:        "access_token= is masked",
			input:       "access_token=longaccesstoken999 expired",
			wantMasked:  []string{"longaccesstoken999"},
			wantPresent: []string{"access_token=***"},
		},
		{
			name:        "no sensitive data passes through unchanged",
			input:       "command failed: exit status 1",
			wantMasked:  []string{},
			wantPresent: []string{"command failed: exit status 1"},
		},
		{
			name:        "empty string",
			input:       "",
			wantMasked:  []string{},
			wantPresent: []string{},
		},
		{
			name:        "multiple secrets in one string",
			input:       "key=secretkey1234 and Bearer tokenvalue5678",
			wantMasked:  []string{"secretkey1234", "tokenvalue5678"},
			wantPresent: []string{"key=***", "Bearer ***"},
		},
		{
			name:        "idempotent on already-sanitized output",
			input:       "sk-*** and key=***",
			wantMasked:  []string{},
			wantPresent: []string{"sk-***", "key=***"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeLog(tt.input)
			for _, banned := range tt.wantMasked {
				if strings.Contains(got, banned) {
					t.Errorf("SanitizeLog(%q) = %q; still contains sensitive value %q", tt.input, got, banned)
				}
			}
			for _, want := range tt.wantPresent {
				if !strings.Contains(got, want) {
					t.Errorf("SanitizeLog(%q) = %q; expected to contain %q", tt.input, got, want)
				}
			}
		})
	}
}
