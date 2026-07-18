package fsx

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// WriteFileAtomically writes a private file and replaces any existing target.
// The platform-specific replace operation preserves Windows compatibility.
func WriteFileAtomically(path string, contents []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	temp, err := os.CreateTemp(dir, filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if err := temp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		_ = temp.Close()
		return fmt.Errorf("secure temporary file: %w", err)
	}
	if _, err := temp.Write(contents); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := replaceFile(tempPath, path); err != nil {
		return fmt.Errorf("replace target file: %w", err)
	}
	return nil
}
