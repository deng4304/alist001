package tool

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrArchiveIllegalPath indicates an archive entry path is unsafe for extraction.
var ErrArchiveIllegalPath = errors.New("archive entry has illegal path")

// SecureJoin returns a safe extraction path for an archive entry.
// It rejects absolute paths, traversal, Windows drive/UNC paths, and NUL bytes.
func SecureJoin(baseDir, entryName string) (string, error) {
	if strings.Contains(entryName, "\x00") {
		return "", fmt.Errorf("%w: %s", ErrArchiveIllegalPath, entryName)
	}

	normalized := strings.ReplaceAll(entryName, "\\", "/")
	if strings.HasPrefix(normalized, "//") {
		return "", fmt.Errorf("%w: %s", ErrArchiveIllegalPath, entryName)
	}
	cleaned := path.Clean(normalized)

	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: %s", ErrArchiveIllegalPath, entryName)
	}
	if strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("%w: %s", ErrArchiveIllegalPath, entryName)
	}

	rel := filepath.FromSlash(cleaned)
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", fmt.Errorf("%w: %s", ErrArchiveIllegalPath, entryName)
	}
	if strings.HasPrefix(rel, `\\`) {
		return "", fmt.Errorf("%w: %s", ErrArchiveIllegalPath, entryName)
	}

	base := filepath.Clean(baseDir)
	dst := filepath.Join(base, rel)

	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("%w: %s (%v)", ErrArchiveIllegalPath, entryName, err)
	}
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return "", fmt.Errorf("%w: %s (%v)", ErrArchiveIllegalPath, entryName, err)
	}

	relCheck, err := filepath.Rel(baseAbs, dstAbs)
	if err != nil {
		return "", fmt.Errorf("%w: %s (%v)", ErrArchiveIllegalPath, entryName, err)
	}
	if relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %s", ErrArchiveIllegalPath, entryName)
	}
	return dst, nil
}
