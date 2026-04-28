package tool

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSecureJoin(t *testing.T) {
	baseDir := t.TempDir()
	tests := []struct {
		name    string
		entry   string
		wantErr bool
	}{
		{name: "ok", entry: "a/b/c.txt", wantErr: false},
		{name: "parent", entry: "../evil.txt", wantErr: true},
		{name: "parent-backslash", entry: "..\\evil.txt", wantErr: true},
		{name: "abs", entry: "/tmp/evil.txt", wantErr: true},
		{name: "drive", entry: "C:\\evil.txt", wantErr: true},
		{name: "unc", entry: "\\\\server\\share\\evil.txt", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dst, err := SecureJoin(baseDir, tc.entry)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tc.entry)
				}
				if !strings.Contains(err.Error(), tc.entry) {
					t.Fatalf("error should include entry name %q, got %q", tc.entry, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.entry, err)
			}
			rel, err := filepath.Rel(baseDir, dst)
			if err != nil {
				t.Fatalf("Rel failed: %v", err)
			}
			if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				t.Fatalf("path escaped baseDir: %q", dst)
			}
		})
	}
}
