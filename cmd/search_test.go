package cmd

import (
	"strings"
	"testing"
)

func TestNormalizeSearchMode(t *testing.T) {
	tests := []struct {
		name       string
		exact      bool
		ignoreCase bool
		textMode   bool
		wantExact  bool
		wantErr    bool
		errSubstr  string
	}{
		{
			name:      "plain fuzzy search unchanged",
			wantExact: false,
		},
		{
			name:      "exact search unchanged",
			exact:     true,
			wantExact: true,
		},
		{
			name:       "ignore-case implies exact",
			ignoreCase: true,
			wantExact:  true,
		},
		{
			name:       "ignore-case keeps explicit exact",
			exact:      true,
			ignoreCase: true,
			wantExact:  true,
		},
		{
			name:       "ignore-case with text mode errors",
			ignoreCase: true,
			textMode:   true,
			wantErr:    true,
			errSubstr:  "not supported with --text",
		},
		{
			name:       "ignore-case with exact and text mode errors",
			exact:      true,
			ignoreCase: true,
			textMode:   true,
			wantErr:    true,
			errSubstr:  "not supported with --text",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotExact, err := normalizeSearchMode(tc.exact, tc.ignoreCase, tc.textMode)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotExact != tc.wantExact {
				t.Fatalf("normalizeSearchMode(%t, %t, %t) = %t, want %t", tc.exact, tc.ignoreCase, tc.textMode, gotExact, tc.wantExact)
			}
		})
	}
}
