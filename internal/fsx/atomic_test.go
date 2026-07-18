package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicallyCreatesAndReplacesFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "state.json")
	if err := WriteFileAtomically(path, []byte("first")); err != nil {
		t.Fatalf("first WriteFileAtomically() error = %v", err)
	}
	if err := WriteFileAtomically(path, []byte("second")); err != nil {
		t.Fatalf("second WriteFileAtomically() error = %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(contents) != "second" {
		t.Fatalf("contents = %q, want second", contents)
	}
}
