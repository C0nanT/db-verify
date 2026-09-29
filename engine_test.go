package main

import (
	"context"
	"errors"
	"testing"
)

// TestProvisionOptsStep_ReportaEDevolveErroDoCtx trava o ponto de checagem
// entre passos do Provision: o progresso é reportado e, se o ctx foi
// cancelado (inclusive pelo próprio Progress), step devolve o erro do ctx.
func TestProvisionOptsStep_ReportaEDevolveErroDoCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var got []string
	opts := ProvisionOpts{Progress: func(format string, a ...any) {
		got = append(got, format)
	}}
	if err := opts.step(ctx, "passo %d", 1); err != nil {
		t.Fatalf("step com ctx vivo: %v", err)
	}

	opts.Progress = func(format string, a ...any) {
		got = append(got, format)
		cancel()
	}
	err := opts.step(ctx, "passo %d", 2)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("step depois do cancelamento = %v, want context.Canceled", err)
	}
	if len(got) != 2 {
		t.Fatalf("Progress chamado %d vez(es), want 2", len(got))
	}
}

// TestProvisionOptsStep_SemProgress garante que step funciona sem callback.
func TestProvisionOptsStep_SemProgress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (ProvisionOpts{}).step(ctx, "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("step = %v, want context.Canceled", err)
	}
}
