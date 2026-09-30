//go:build docker

// Package conformance guarda o registro de fixtures e os helpers da suíte de
// conformidade (TestEngineConformance, na raiz): um único corpo de teste,
// parametrizado pelas engines registradas, sem ramificação por nome de
// engine. Cada engine cadastra aqui, de um init(), como gerar seu backup de
// teste mínimo (válido e truncado).
//
// Fica atrás da build tag "docker" porque só a suíte com Docker o consome:
// assim as fixtures deixam de precisar morar em arquivos _test.go (que não
// são compilados quando outro pacote os importa) sem entrar no binário de
// produção. A exceção é testdata.go, usado também pelos testes unitários.
package conformance

import "testing"

// ConformanceBackup é o que BuildValid devolve: o caminho do backup mínimo
// gerado (duas coleções — uma com linhas e uma vazia, uma com coluna de data
// e uma sem) e o oráculo contra o qual o corpo genérico compara o que a
// engine devolveu.
type ConformanceBackup struct {
	Path string
	// WantCollections é o conjunto exato de coleções esperadas, com a
	// contagem exata de cada uma (a coleção vazia entra com 0).
	WantCollections map[string]int64
	// DateCollection é o nome da coleção com coluna de data, usada para
	// verificar o limite de 20 linhas e a ordem decrescente de Recent.
	// Precisa ter mais de 20 linhas para exercitar o limite de verdade.
	DateCollection string
	// DateColumn é a coluna usada para ordenar DateCollection; os valores
	// que Recent devolve para ela precisam ordenar corretamente como string
	// (ex.: "AAAA-MM-DD[ HH:MM:SS]").
	DateColumn string
}

// ConformanceFixture é o que cada engine declara sobre como gerar seus
// backups de teste. O corpo de teste genérico só chama essas funções; toda
// lógica específica de engine (como gerar um dump, como corrompê-lo, o que é
// um comando nativo válido/inválido) fica contida na fixture.
type ConformanceFixture struct {
	// BuildValid gera o backup mínimo conhecido descrito no pacote.
	BuildValid func(t *testing.T) ConformanceBackup
	// BuildTruncated gera um backup deliberadamente truncado/corrompido, que
	// deve produzir erros de restore reportados, não engolidos.
	BuildTruncated func(t *testing.T) string
	// ValidQuery é um comando nativo que deve devolver resultado sem erro.
	ValidQuery string
	// InvalidQuery é um comando nativo malformado, que deve devolver erro
	// sem pânico.
	InvalidQuery string
}

var fixtures = map[string]ConformanceFixture{}

// Register cadastra a fixture de conformidade de uma engine, pelo nome
// devolvido por Engine.Name(). Chamado do init() do arquivo de fixture de
// cada engine.
func Register(engine string, f ConformanceFixture) {
	fixtures[engine] = f
}

// Lookup devolve a fixture cadastrada para a engine, e false se nenhuma foi.
func Lookup(engine string) (ConformanceFixture, bool) {
	f, ok := fixtures[engine]
	return f, ok
}
