package engine

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
	if err := opts.Step(ctx, "passo %d", 1); err != nil {
		t.Fatalf("step com ctx vivo: %v", err)
	}

	opts.Progress = func(format string, a ...any) {
		got = append(got, format)
		cancel()
	}
	err := opts.Step(ctx, "passo %d", 2)
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
	if err := (ProvisionOpts{}).Step(ctx, "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("step = %v, want context.Canceled", err)
	}
}

// TestCollection_Qualified caracteriza a concatenação namespace.nome.
func TestCollection_Qualified(t *testing.T) {
	c := Collection{Namespace: "public", Name: "pedidos"}
	if got, want := c.Qualified(), "public.pedidos"; got != want {
		t.Errorf("Qualified() = %q, want %q", got, want)
	}
}

// fakeEngine é o mínimo de Engine para exercitar o registro.
type fakeEngine struct{ name string }

func (f fakeEngine) Name() string                        { return f.name }
func (f fakeEngine) Detect([]byte, string) (Match, bool) { return Match{}, false }
func (f fakeEngine) Expects() string                     { return "" }
func (f fakeEngine) Provision(context.Context, *Backup, ProvisionOpts) (Session, error) {
	return nil, errors.New("fake")
}

// TestRegistry trava o contrato do registro: Engines devolve na ordem de
// registro (é ela que desempata a detecção), a fatia devolvida é uma cópia,
// e Lookup acha pelo nome ou diz que não achou.
func TestRegistry(t *testing.T) {
	saved := registry
	registry = nil
	t.Cleanup(func() { registry = saved })

	Register(fakeEngine{"b"})
	Register(fakeEngine{"a"})

	got := Engines()
	if len(got) != 2 || got[0].Name() != "b" || got[1].Name() != "a" {
		t.Fatalf("Engines() = %v, want [b a] na ordem de registro", got)
	}
	got[0] = fakeEngine{"x"}
	if Engines()[0].Name() != "b" {
		t.Fatal("alterar a fatia de Engines() mudou o registro")
	}

	if e, ok := Lookup("a"); !ok || e.Name() != "a" {
		t.Fatalf("Lookup(a) = %v, %v", e, ok)
	}
	if _, ok := Lookup("nenhuma"); ok {
		t.Fatal("Lookup(nenhuma) achou algo")
	}
}

// TestRegister_NomeDuplicadoEntraEmPanico: um Name() repetido (copiar e
// colar uma engine) não pode entrar calado no registro.
func TestRegister_NomeDuplicadoEntraEmPanico(t *testing.T) {
	saved := registry
	registry = nil
	t.Cleanup(func() { registry = saved })

	Register(fakeEngine{"a"})
	defer func() {
		if recover() == nil {
			t.Fatal("Register com nome duplicado não entrou em pânico")
		}
		if len(registry) != 1 {
			t.Fatalf("registro tem %d engines depois do pânico, want 1", len(registry))
		}
	}()
	Register(fakeEngine{"a"})
}
