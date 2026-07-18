//go:build windows

package fsx

import "golang.org/x/sys/windows"

func replaceFile(oldPath, newPath string) error {
	return windows.Rename(oldPath, newPath)
}
