package main

import (
	"github.com/C0nanT/db-verify/engines/mongo"
	"github.com/C0nanT/db-verify/engines/mysql"
	"github.com/C0nanT/db-verify/engines/postgres"
	"github.com/C0nanT/db-verify/engines/redis"
	"github.com/C0nanT/db-verify/engines/sqlite"
	"github.com/C0nanT/db-verify/internal/engine"
)

// Esta é a única lista de engines do binário. A ordem importa:
// no desempate de detecção (mesma confiança), vence a engine que aparece
// primeiro aqui. Mantenha-a estável ao adicionar engines novas (no fim, ou
// onde o desempate desejado exigir). Esse desempate é só rede de segurança:
// TestNoDetectTies (detect_test.go) proíbe empate nas fixtures de
// testdata/headers/, e engine.Register entra em pânico com nome duplicado.
//
// É um inicializador de variável de pacote, não um init() nem um trecho de
// main(): assim roda antes de qualquer uso do registro tanto no binário
// quanto nos testes da raiz, onde main() não executa.
var _ = registrarEngines(
	mysql.MariaDBEngine{},
	mongo.Engine{},
	mysql.Engine{},
	postgres.Engine{},
	redis.Engine{},
	sqlite.Engine{},
)

func registrarEngines(engines ...engine.Engine) struct{} {
	for _, e := range engines {
		engine.Register(e)
	}
	return struct{}{}
}
