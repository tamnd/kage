//go:build !webview

package viewer

import (
	"context"
	"testing"
	"time"
)

func TestNativeIsFalseInDefaultBuild(t *testing.T) {
	if Native {
		t.Fatal("Native should be false without the webview build tag")
	}
}

func TestLockMainThreadIsNoop(t *testing.T) {
	// Must not panic; there is no native UI to pin to.
	LockMainThread()
}

func TestShowReturnsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// Browser:false so no system browser is launched during the test.
	go func() { done <- Show(ctx, Options{URL: "http://127.0.0.1:0", Browser: false}) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Show returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Show did not return after context cancellation")
	}
}
