package main

import (
	"context"
	"fmt"
	"net"
	"time"
)

// Match descreve o que uma engine reconheceu num candidato a backup: o
// formato específico dela, a versão de origem (se o formato carrega essa
// informação), o banco de origem, e uma confiança para o registro desempatar
// entre engines que reivindicam o mesmo arquivo.
type Match struct {
	Format     string
	Version    string
	OriginDB   string
	Confidence int // 100 = magic bytes; 50 = extensão; 10 = palpite
}

// Níveis de confiança padronizados que as engines usam em Detect, na ordem
// em que devem vencer numa disputa: magic bytes > extensão > palpite.
const (
	ConfidenceMagic     = 100
	ConfidenceExtension = 50
	ConfidenceGuess     = 10
)

// Backup descreve um arquivo de backup já inspecionado: o que sabemos antes
// de qualquer container subir.
type Backup struct {
	Path        string
	Size        int64
	Compression string
	Engine      string
	Format      string
	Version     string
	OriginDB    string
	// Guessed é true quando a engine venceu por palpite (Confidence <=
	// guessConfidence), não por magic bytes ou extensão — a ambiguidade
	// conhecida do .sql sem cabeçalho identificável. Forced é true quando o
	// operador escolheu a engine via --engine, pulando a disputa entre
	// engines.
	Guessed bool
	Forced  bool
}

// ProvisionOpts carrega os parâmetros genéricos que hoje vêm de flags de
// linha de comando. Cada engine decide o que faz sentido usar.
type ProvisionOpts struct {
	VersionTag  string
	Port        int
	Jobs        int
	DBName      string
	ExactCounts bool
	// Progress, quando não nil, é chamado com mensagens de progresso durante
	// o provisionamento (subir container, aguardar, copiar, restaurar…),
	// para o chamador (main.go) imprimir na mesma ordem de sempre.
	Progress func(format string, a ...any)
}

func (o ProvisionOpts) report(format string, a ...any) {
	if o.Progress != nil {
		o.Progress(format, a...)
	}
}

// Collection é uma linha do painel esquerdo: uma tabela, uma coleção do
// Mongo, um grupo de chaves do Redis... Namespace/Name/Count/Size são o que
// a TUI usa diretamente. Hint é uma descrição curta e já pronta para exibir
// de como a engine escolhe "os mais recentes" para esta coleção (ex.:
// "ordenado por created_at (data)"). Preview é o texto nativo da consulta
// que Recent vai rodar, publicado com antecedência para a TUI mostrar
// enquanto a consulta de verdade ainda está em voo. Descriptor é opaco: só a
// Session que criou a Collection sabe interpretá-lo, e é isso que ela recebe
// de volta em Recent.
type Collection struct {
	Namespace  string
	Name       string
	Count      int64
	Size       string
	Hint       string
	Preview    string
	Descriptor any
}

// Qualified é o nome de exibição da coleção: "namespace.nome", ou só "nome"
// quando não há namespace (ex.: schema padrão da engine).
func (c Collection) Qualified() string {
	if c.Namespace == "" {
		return c.Name
	}
	return c.Namespace + "." + c.Name
}

// HealthField é um par rótulo/valor que uma engine publica sobre o backup
// restaurado. Cada engine decide o que faz sentido publicar; campos que não
// se aplicam simplesmente não entram na lista.
type HealthField struct {
	Label string
	Value string
}

// Health é o resumo geral do backup restaurado.
type Health struct {
	Name   string
	Size   string
	Fields []HealthField
}

// ResultSet é um resultado de consulta já formatado como texto, pronto para
// a TUI exibir sem interpretar.
type ResultSet struct {
	Columns  []string
	Rows     [][]string
	Query    string
	Language string // "sql", "mongo", "redis"…
	Elapsed  time.Duration
}

// ConnectHint diz ao operador como continuar investigando à mão depois que a
// TUI fecha (ou com --keep).
type ConnectHint struct {
	Name      string // nome do container/sessão; "" quando não há container
	DSN       string
	Shell     string // comando de cliente direto contra a DSN
	ExecShell string // comando via docker exec, para quando o cliente não está no host
	Remove    string // comando para remover manualmente
	Port      int
}

// RestoreResult resume o que aconteceu no passo de restore dentro de
// Provision: erros, duração, log completo em arquivo e exit code.
type RestoreResult struct {
	Errors   []string
	Duration time.Duration
	LogPath  string
	ExitCode int
}

// Engine sabe reconhecer e provisionar um tipo de backup.
type Engine interface {
	Name() string
	// Detect recebe o cabeçalho já descomprimido e o caminho original.
	// Devolve a confiança para o registro desempatar entre engines.
	Detect(head []byte, path string) (Match, bool)
	// Expects descreve, numa linha, o que esta engine espera reconhecer
	// (assinaturas/extensões aceitas). Usado nas mensagens de erro quando
	// nenhuma engine reconhece um arquivo, e em --list-engines.
	Expects() string
	// Provision é deliberadamente grosso: sobe o container, espera ficar
	// pronto, copia o backup, restaura e conecta — para o número de seams
	// no projeto continuar sendo um.
	Provision(ctx context.Context, b *Backup, opts ProvisionOpts) (Session, error)
}

// Session é a conexão viva com o backup restaurado.
type Session interface {
	Health(ctx context.Context) (*Health, error)
	Collections(ctx context.Context, exact bool) ([]Collection, error)
	Recent(ctx context.Context, c Collection) (*ResultSet, error)
	Query(ctx context.Context, raw string) (*ResultSet, error)
	ConnectHint() ConnectHint
	// Restore devolve o resultado do restore feito dentro de Provision.
	Restore() *RestoreResult
	Close() error
}

// ------------------------------------------------------------- registry ---

var registry []Engine

// Register cadastra uma engine. Chamado do init() do arquivo de cada engine.
func Register(e Engine) {
	registry = append(registry, e)
}

// Engines devolve as engines cadastradas, na ordem de registro.
func Engines() []Engine {
	return append([]Engine(nil), registry...)
}

// Lookup busca uma engine pelo nome.
func Lookup(name string) (Engine, bool) {
	for _, e := range registry {
		if e.Name() == name {
			return e, true
		}
	}
	return nil, false
}

// freePortFrom procura a primeira porta livre a partir de start (inclusive),
// dentro de uma janela pequena — usada por engines cujo container publica
// uma porta padrão conhecida (ex.: MySQL a partir de 3306), para o
// comportamento ficar previsível: a porta padrão quando ela estiver livre,
// a próxima disponível caso contrário.
func freePortFrom(start int) int {
	for p := start; p < start+100; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			l.Close()
			return p
		}
	}
	return start
}
