package main

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
	"fmt"
	"os"
	"regexp"
	"time"
)

func init() { Register(mariadbEngine{}) }

// mariadbEngine implementa Engine para MariaDB.
type mariadbEngine struct{}

func (mariadbEngine) Name() string { return "mariadb" }

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
func (mariadbEngine) Detect(head []byte, path string) (Match, bool) {
	if !reMariaDBDumpHeader.Match(head) {
		return Match{}, false
	}
	m := Match{Format: "sql", Confidence: ConfidenceMagic}
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
func (mariadbEngine) Expects() string {
	return `dumps do mariadb-dump: cabeçalho "-- MariaDB dump" (sem fallback de extensão — .sql sem cabeçalho vai para o MySQL)`
}

// Provision sobe o container, espera ficar pronto, copia o dump, restaura e
// conecta — mesmo formato grosso das demais engines. Reusa mysqlContainer e
// mysqlSession inteiros; só Image e Client mudam.
func (mariadbEngine) Provision(ctx context.Context, b *Backup, opts ProvisionOpts) (Session, error) {
	if err := dockerAvailable(); err != nil {
		return nil, err
	}

	version := mariadbResolveVersion(opts.VersionTag, b.Version)
	port := opts.Port
	if port == 0 {
		port = freePortFrom(mysqlDefaultPort)
	}
	db := opts.DBName
	if db == "" {
		db = "verify"
	}

	cont := &mysqlContainer{
		Name:   fmt.Sprintf("db-verify-%d", os.Getpid()),
		Image:  "mariadb:" + version,
		Client: "mariadb",
		Port:   port, DB: db, User: "root", Pass: "root",
	}

	opts.report("subindo container %s (imagem %s)…", cont.Name, cont.Image)
	finalPort, err := startWithPortRetry(ctx, cont.Name, port, func(p int) error {
		cont.Port = p
		return cont.Start(ctx)
	})
	if err != nil {
		return nil, err
	}
	if finalPort != port {
		opts.report("porta %d livre, usando essa…", finalPort)
	}
	opts.report("aguardando o MariaDB ficar pronto…")
	if err := cont.WaitReady(ctx, 120*time.Second); err != nil {
		cont.Remove()
		return nil, err
	}
	opts.report("copiando dump para o container…")
	if err := cont.CopyDump(ctx, b); err != nil {
		cont.Remove()
		return nil, err
	}
	opts.report("restaurando (pode demorar)…")
	res, err := cont.Restore(ctx)
	if err != nil {
		cont.Remove()
		return nil, err
	}

	conn, err := mysqlConnect(cont.DSN())
	if err != nil {
		cont.Remove()
		return nil, fmt.Errorf("conexão falhou: %w", err)
	}

	return &mysqlSession{db: conn, cont: cont, restore: res}, nil
}
