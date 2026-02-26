package cli

import (
	"errors"
	"net"
	"testing"
)

func TestRunWatchText_IntentionalCloseReturnsNil(t *testing.T) {
	err := watchTextExitErr(true, net.ErrClosed)
	if err != nil {
		t.Fatalf("expected nil error for intentional close, got %v", err)
	}
}

func TestRunWatchText_UnexpectedErrorReturnsError(t *testing.T) {
	want := errors.New("boom")
	err := watchTextExitErr(false, want)
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}
