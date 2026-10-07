package probe

import "testing"

func TestNormalizeFFProbeContainerFromFileExtension(t *testing.T) {
	t.Parallel()
	tests := []struct{ raw, path, want string }{
		{"mov", "Synthetic Film (2026).mp4", "mp4"},
		{"mov", "Film.m4v", "mp4"},
		{"mov", "QuickTime.mov", "mov"},
		{"matroska", "Film.mkv", "matroska"},
		{"webm", "Film.webm", "webm"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if got := normalizeContainer(tc.raw, tc.path); got != tc.want {
				t.Fatalf("normalizeContainer(%q,%q)=%q want %q", tc.raw, tc.path, got, tc.want)
			}
		})
	}
}
