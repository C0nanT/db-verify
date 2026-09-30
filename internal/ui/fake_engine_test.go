package ui

// Engine fictícia para os testes do seletor: o pacote ui não importa engines
// concretas, então o registro global começa vazio aqui. Ela imita o Postgres
// (nome "postgres", reconhece qualquer arquivo por palpite), que é o que as
// asserções do seletor esperam.

import (
	"context"

	"db-verify/internal/engine"
)

type fakePostgres struct{}

func (fakePostgres) Name() string { return "postgres" }
func (fakePostgres) Detect(head []byte, path string) (engine.Match, bool) {
	return engine.Match{Format: "fake", Confidence: engine.ConfidenceGuess}, true
}
func (fakePostgres) Expects() string { return "qualquer arquivo (palpite)" }
func (fakePostgres) Provision(ctx context.Context, b *engine.Backup, opts engine.ProvisionOpts) (engine.Session, error) {
	panic("fakePostgres.Provision não deveria ser chamado nos testes do seletor")
}

func init() { engine.Register(fakePostgres{}) }
