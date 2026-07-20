//go:build !darwin

package main

// preventAppNap is a no-op outside macOS; other platforms do not suspend
// background apps the way macOS App Nap does.
func preventAppNap() {}
