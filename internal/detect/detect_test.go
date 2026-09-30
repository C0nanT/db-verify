package detect

// Testes da disputa entre engines por confiança e do erro de "nenhuma engine
// reconheceu", com engines fictícias (fakeEngine) em vez do registro global:
// só o contrato de internal/engine é necessário.

import (
	"context"
	"strings"
	"testing"

	"db-verify/internal/engine"
)

// fakeEngine é uma engine mínima para testar a disputa de confiança sem
// depender do registro global nem de uma engine de verdade.
type fakeEngine struct {
	name    string
	detect  func(head []byte, path string) (engine.Match, bool)
	expects string
}

func (f fakeEngine) Name() string { return f.name }
func (f fakeEngine) Detect(head []byte, path string) (engine.Match, bool) {
	if f.detect == nil {
		return engine.Match{}, false
	}
	return f.detect(head, path)
}
func (f fakeEngine) Expects() string { return f.expects }
func (f fakeEngine) Provision(ctx context.Context, b *engine.Backup, opts engine.ProvisionOpts) (engine.Session, error) {
	panic("fakeEngine.Provision não deveria ser chamado em teste de detecção")
}

func matchAlways(format string, confidence int) func([]byte, string) (engine.Match, bool) {
	return func(head []byte, path string) (engine.Match, bool) {
		return engine.Match{Format: format, Confidence: confidence}, true
	}
}

func matchNever(head []byte, path string) (engine.Match, bool) { return engine.Match{}, false }

// TestChooseEngine_MagicVenceExtensaoVencePalpite caracteriza o desempate
// por confiança: magic bytes (100) > extensão (50) > palpite (10),
// independentemente da ordem em que as engines estão na lista.
func TestChooseEngine_MagicVenceExtensaoVencePalpite(t *testing.T) {
	magic := fakeEngine{name: "magic", detect: matchAlways("m", engine.ConfidenceMagic)}
	ext := fakeEngine{name: "ext", detect: matchAlways("e", engine.ConfidenceExtension)}
	guess := fakeEngine{name: "guess", detect: matchAlways("g", engine.ConfidenceGuess)}

	eng, m, ok := chooseEngine([]engine.Engine{guess, ext, magic}, nil, "arquivo")
	if !ok || eng.Name() != "magic" || m.Format != "m" {
		t.Fatalf("esperava magic vencer, tive engine=%v format=%q ok=%v", eng, m.Format, ok)
	}

	eng, _, ok = chooseEngine([]engine.Engine{guess, magic, ext}, nil, "arquivo")
	if !ok || eng.Name() != "magic" {
		t.Fatalf("esperava magic vencer independente da ordem, tive %v", eng)
	}

	eng, _, ok = chooseEngine([]engine.Engine{guess, ext}, nil, "arquivo")
	if !ok || eng.Name() != "ext" {
		t.Fatalf("esperava extensão vencer palpite, tive %v", eng)
	}
}

// TestChooseEngine_EmpateResolvePorOrdemDeRegistro: duas engines com a
// mesma confiança — a primeira da lista (ordem de registro) vence.
func TestChooseEngine_EmpateResolvePorOrdemDeRegistro(t *testing.T) {
	first := fakeEngine{name: "primeira", detect: matchAlways("f", engine.ConfidenceMagic)}
	second := fakeEngine{name: "segunda", detect: matchAlways("s", engine.ConfidenceMagic)}

	eng, _, ok := chooseEngine([]engine.Engine{first, second}, nil, "arquivo")
	if !ok || eng.Name() != "primeira" {
		t.Fatalf("esperava a primeira registrada vencer o empate, tive %v", eng)
	}

	// invertendo a ordem de registro, a vencedora muda junto.
	eng, _, ok = chooseEngine([]engine.Engine{second, first}, nil, "arquivo")
	if !ok || eng.Name() != "segunda" {
		t.Fatalf("esperava a primeira da lista (agora segunda) vencer, tive %v", eng)
	}
}

// TestChooseEngine_NenhumaReconhece: quando nenhuma engine da lista
// reconhece o cabeçalho, chooseEngine devolve ok=false, e noEngineErr monta
// um erro nomeando as engines disponíveis e o que cada uma espera.
func TestChooseEngine_NenhumaReconhece(t *testing.T) {
	a := fakeEngine{name: "aa", detect: matchNever, expects: "cabeçalho AA"}
	b := fakeEngine{name: "bb", detect: matchNever, expects: "cabeçalho BB"}

	_, _, ok := chooseEngine([]engine.Engine{a, b}, []byte("lixo"), "arquivo.bin")
	if ok {
		t.Fatalf("esperava ok=false quando nenhuma engine reconhece")
	}

	err := noEngineErr([]engine.Engine{a, b})
	if err == nil {
		t.Fatalf("esperava erro não nulo")
	}
	msg := err.Error()
	for _, want := range []string{"aa", "cabeçalho AA", "bb", "cabeçalho BB"} {
		if !strings.Contains(msg, want) {
			t.Errorf("mensagem de erro não menciona %q: %s", want, msg)
		}
	}
}
