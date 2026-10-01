package mysql

// Implementação da engine MariaDB. É deliberadamente um arquivo fino: todo o
// container, a sessão, a heurística de coluna de ordenação e as consultas de
// introspecção são as mesmas de mysql.go (mysqlContainer, mysqlSession,
// chooseOrderColumn, mysqlTablesSQL, mysqlColumnsSQL, mysqlSinglePKSQL,
// mysqlHealthSQL, mysqlRecentQuery, mysqlFormatValue…) — o protocolo de fio e
// o information_schema são compatíveis o bastante entre MySQL e MariaDB para
// uma implementação relacional só. O que muda, e é só o que este arquivo
// declara, é: (1) o cabeçalho que identifica um dump como MariaDB em vez de
// MySQL; (2) a imagem Docker (mariadb:<v> em vez de mysql:<v>); (3) o
// binário de cliente usado para restore/shell (mariadb em vez de mysql); (4)
// a versão default quando o dump não informa nenhuma.
//
// MySQL e MariaDB continuam sendo engines distintas no registro (SPEC.md,
// "Decisões específicas") porque um dump com cabeçalho MariaDB reconhecido
// deve restaurar na imagem mariadb:<v>, não mysql:<v> — as duas imagens têm
// binários e comportamento de inicialização próprios, e restaurar um dump
// MariaDB numa imagem MySQL (ou vice-versa) pode falhar de formas sutis que
// só aparecem em produção.

import (
	"context"
	"regexp"

	"db-verify/internal/docker"
	"db-verify/internal/engine"
)

// MariaDBEngine implementa Engine para MariaDB. Host é o acesso à CLI
// Docker; nil usa o Docker de produção.
type MariaDBEngine struct {
	Host *docker.DockerHost
}

func (MariaDBEngine) Name() string { return "mariadb" }

// reMariaDBDumpHeader reconhece o cabeçalho de texto que o mariadb-dump
// sempre escreve na primeira linha — "-- MariaDB dump", distinto do "--
// MySQL dump" do mysqldump (mysql.go). É a única diferença de reconhecimento
// entre as duas engines: sem esse cabeçalho, um .sql cai na ambiguidade já
// documentada em SPEC.md ("Detecção") e resolvida a favor do MySQL — este
// arquivo não reivindica extensão nenhuma, de propósito, para não reabrir
// essa disputa.
var reMariaDBDumpHeader = regexp.MustCompile(`-- MariaDB dump\b`)

// defaultMariaDBVersion é o fallback quando nem "-- Server version" nem
// "Distrib" informam a versão de origem no cabeçalho: a última série LTS
// estável da MariaDB no momento desta entrega.
const defaultMariaDBVersion = "10.11"

// mariadbResolveVersion segue a mesma ordem de precedência do MySQL
// (resolveMySQLFamilyVersion, mysql.go), só com o fallback do MariaDB.
func mariadbResolveVersion(versionTag, backupVersion string) string {
	return resolveMySQLFamilyVersion(versionTag, backupVersion, defaultMariaDBVersion)
}

// Detect reconhece um mariadb-dump em texto plano pelo cabeçalho "--
// MariaDB dump", e extrai versão e banco de origem com as mesmas expressões
// do MySQL (reMySQLFamilyServerVer/reMySQLFamilyDistribVer/
// reMySQLFamilyDatabaseLine, mysql.go) — o formato das linhas de metadado é
// idêntico, só o texto de identificação do cabeçalho muda.
func (MariaDBEngine) Detect(head []byte, path string) (engine.Match, bool) {
	if !reMariaDBDumpHeader.Match(head) {
		return engine.Match{}, false
	}
	m := engine.Match{Format: "sql", Confidence: engine.ConfidenceMagic}
	if mm := reMySQLFamilyServerVer.FindSubmatch(head); mm != nil {
		m.Version = string(mm[1])
	} else if mm := reMySQLFamilyDistribVer.FindSubmatch(head); mm != nil {
		m.Version = string(mm[1])
	}
	if mm := reMySQLFamilyDatabaseLine.FindSubmatch(head); mm != nil {
		m.OriginDB = string(mm[1])
	}
	return m, true
}

// Expects descreve o que o MariaDB reconhece, para mensagens de erro e
// --list-engines. Sem extensão: um .sql sem cabeçalho reconhecível é
// atribuído ao MySQL (ambiguidade documentada em SPEC.md), não ao MariaDB.
func (MariaDBEngine) Expects() string {
	return `dumps do mariadb-dump: cabeçalho "-- MariaDB dump" (sem fallback de extensão — .sql sem cabeçalho vai para o MySQL)`
}

// Provision sobe o container, espera ficar pronto, copia o dump, restaura e
// conecta — mesmo formato grosso das demais engines. Reusa
// provisionMySQLFamily (mysqlContainer e mysqlSession inteiros); só Image e
// Client mudam.
func (e MariaDBEngine) Provision(ctx context.Context, b *engine.Backup, opts engine.ProvisionOpts) (engine.Session, error) {
	version := mariadbResolveVersion(opts.VersionTag, b.Version)
	return provisionMySQLFamily(ctx, e.Host, b, opts, "mariadb:"+version, "mariadb", "MariaDB")
}
