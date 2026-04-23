package cmd

import "testing"

func TestMatchAnyPath(t *testing.T) {
	tests := []struct {
		name  string
		rel   string
		globs []string
		want  bool
	}{
		{
			name:  "recursive double star matches nested path",
			rel:   "internal/updatecheck/updatecheck.go",
			globs: []string{"internal/**"},
			want:  true,
		},
		{
			name:  "double star suffix matches nested extension",
			rel:   "src/features/video-editor/components/timeline/TimelineObject.tsx",
			globs: []string{"**/*.tsx"},
			want:  true,
		},
		{
			name:  "double star in the middle spans directories",
			rel:   "src/features/video-editor/components/timeline/types.ts",
			globs: []string{"src/**/types.ts"},
			want:  true,
		},
		{
			name:  "single star stays within one segment",
			rel:   "src/features/video-editor/components/timeline/TimelineObject.tsx",
			globs: []string{"src/*"},
			want:  false,
		},
		{
			name:  "plain path keeps substring matching",
			rel:   "cmd/search.go",
			globs: []string{"cmd/search"},
			want:  true,
		},
		{
			name:  "leading dot slash is normalized for globs",
			rel:   "./tools/file_batch_test.go",
			globs: []string{"tools/*_test.go"},
			want:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchAnyPath(tc.rel, tc.globs)
			if got != tc.want {
				t.Fatalf("matchAnyPath(%q, %q) = %t, want %t", tc.rel, tc.globs, got, tc.want)
			}
		})
	}
}
