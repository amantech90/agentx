//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Foundation
#include "appnap_darwin.h"
*/
import "C"

// preventAppNap stops macOS from throttling the app when it is in the
// background, so paired devices stay connected instead of dropping offline
// after a few minutes. The Objective-C implementation lives in appnap_darwin.m.
func preventAppNap() {
	C.agentxDisableAppNap()
}
