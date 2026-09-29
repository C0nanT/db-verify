package main

import (
	"context"
	"errors"
	"testing"
)

// TestInterruptedErr: erro com ctx cancelado vira errInterrupted (saída 130);
// erro com ctx vivo passa intacto.
func TestInterruptedErr(t *testing.T) {
	boom := errors.New("boom")

	if got := interruptedErr(context.Background(), boom); got != boom {
		t.Fatalf("ctx vivo: got %v, want o erro original", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := interruptedErr(ctx, boom)
	if !errors.Is(got, errInterrupted) {
		t.Fatalf("ctx cancelado: got %v, want errInterrupted", got)
	}
}
