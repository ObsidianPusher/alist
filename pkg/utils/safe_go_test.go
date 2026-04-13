package utils

import (
	"testing"
	"time"
)

func TestRecoverPanic(t *testing.T) {
	func() {
		defer RecoverPanic("test")
		panic("panic for recover")
	}()
}

func TestGoSafe(t *testing.T) {
	done := make(chan struct{})
	GoSafe("test-gosafe", func() {
		close(done)
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("GoSafe did not execute function in time")
	}
}

func TestGoSafeRecoverPanic(t *testing.T) {
	GoSafe("test-gosafe-panic", func() {
		panic("panic in gosafe")
	})
	time.Sleep(50 * time.Millisecond)
}
