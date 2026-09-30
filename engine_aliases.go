package main

// Apelidos TEMPORÁRIOS para o contrato que agora mora em internal/engine e
// repasses para internal/relational (ao fim do arquivo).
// Existem só para que o código que ainda está no pacote main (engines,
// detecção, TUI, testes) continue compilando sem ser tocado enquanto a
// migração para pacotes internos anda. Saem no ticket 13 da migração, quando
// todo o código restante importar o pacote engine diretamente. Não adicione
// nada novo aqui.

import (
	"io"

	"db-verify/internal/detect"
	"db-verify/internal/engine"
	"db-verify/internal/relational"
)

type (
	Match         = engine.Match
	Backup        = engine.Backup
	ProvisionOpts = engine.ProvisionOpts
	Collection    = engine.Collection
	HealthField   = engine.HealthField
	Health        = engine.Health
	ResultSet     = engine.ResultSet
	ConnectHint   = engine.ConnectHint
	RestoreResult = engine.RestoreResult
	Engine        = engine.Engine
	Session       = engine.Session
)

const (
	ConfidenceMagic     = engine.ConfidenceMagic
	ConfidenceExtension = engine.ConfidenceExtension
	ConfidenceGuess     = engine.ConfidenceGuess
)

// Register repassa para engine.Register.
func Register(e Engine) { engine.Register(e) }

// Engines repassa para engine.Engines.
func Engines() []Engine { return engine.Engines() }

// Lookup repassa para engine.Lookup.
func Lookup(name string) (Engine, bool) { return engine.Lookup(name) }

// Repasses TEMPORÁRIOS para o pacote internal/relational, pelo mesmo motivo
// dos apelidos acima: as engines que ainda estão em main os usam pelos nomes
// antigos. Saem junto com o movimento de cada engine.

type relationalColumn = relational.Column

var orderColumnTiers = relational.OrderColumnTiers

func chooseRelationalOrderColumn(cols []relationalColumn, pk string, dateTypes map[string]bool) (string, bool) {
	return relational.ChooseOrderColumn(cols, pk, dateTypes)
}

func sqlStringList(names []string) string { return relational.SQLStringList(names) }

func orderHint(col string, byDate bool) string { return relational.OrderHint(col, byDate) }

// Repasses TEMPORÁRIOS para o pacote internal/detect, pelo mesmo motivo: o
// código que ainda está em main (engines, TUI, testes) usa os nomes antigos.

func InspectDump(path string) (*Backup, error) { return detect.InspectDump(path) }

func InspectDumpAs(path, forceEngine string) (*Backup, error) {
	return detect.InspectDumpAs(path, forceEngine)
}

func openMaybeCompressed(path string) (io.ReadCloser, string, error) {
	return detect.OpenMaybeCompressed(path)
}

func unknownEngineErr(name string) error { return detect.UnknownEngineErr(name) }
