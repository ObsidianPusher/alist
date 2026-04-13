package utils

import "runtime/debug"

func RecoverPanic(scope string) {
	if r := recover(); r != nil {
		if scope == "" {
			scope = "unknown"
		}
		Log.Errorf("panic recovered in %s: %v\n%s", scope, r, debug.Stack())
	}
}

func GoSafe(scope string, fn func()) {
	go func() {
		defer RecoverPanic(scope)
		fn()
	}()
}
