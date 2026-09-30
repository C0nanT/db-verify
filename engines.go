package main

import (
	"db-verify/engines/redis"
	"db-verify/engines/sqlite"
)

// Esta é a única lista de engines do binário. A ordem importa:
// no desempate de detecção (mesma confiança), vence a engine que aparece
// primeiro aqui. Mantenha-a estável ao adicionar engines novas (no fim, ou
// onde o desempate desejado exigir).
//
// É um inicializador de variável de pacote, não um init() nem um trecho de
// main(): assim roda antes de qualquer uso do registro tanto no binário
// quanto nos testes da raiz, onde main() não executa.
var _ = registrarEngines(
	mariadbEngine{},
	mongoEngine{},
	mysqlEngine{},
	pgEngine{},
	redis.Engine{},
	sqlite.Engine{},
)

func registrarEngines(engines ...Engine) struct{} {
	for _, e := range engines {
		Register(e)
	}
	return struct{}{}
}
