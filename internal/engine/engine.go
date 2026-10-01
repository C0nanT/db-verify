// Package engine é o contrato das engines de banco: as interfaces
// Engine/Session, os tipos que circulam entre elas e quem as chama, e o
// registro (Register/Engines/Lookup). Não importa nenhum outro pacote do
// projeto.
package engine

import (
	"context"
	"fmt"
	"os"
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
	Confidence int // um dos níveis ConfidenceMagic, ConfidenceExtension ou ConfidenceGuess
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
	// ConfidenceGuess), não por magic bytes ou extensão — a ambiguidade
	// conhecida do .sql sem cabeçalho identificável. Forced é true quando o
	// operador escolheu a engine via --engine, pulando a disputa entre
	// engines.
	Guessed bool
	Forced  bool
}

// ProvisionOpts carrega os parâmetros genéricos que hoje vêm de flags de
// linha de comando. Cada engine decide o que faz sentido usar.
type ProvisionOpts struct {
	VersionTag string
	Port       int
	Jobs       int
	DBName     string
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

// Step reporta o início de um passo do Provision e devolve ctx.Err(): é o
// ponto de checagem entre passos, para que um cancelamento (Ctrl+C) vire
// erro em vez de o Provision seguir para o próximo passo. Quem recebe erro
// daqui limpa o que já criou antes de devolvê-lo.
func (o ProvisionOpts) Step(ctx context.Context, format string, a ...any) error {
	o.report(format, a...)
	return ctx.Err()
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

// HumanSize formata um tamanho em bytes para exibição ("512 B", "1.5 MB"),
// como o campo Size de Collection.
func HumanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
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

// DiscardLog apaga o log do restore em arquivo, se houver. Usado quando o
// Provision falha (ou é cancelado) depois do restore: o log é um recurso
// externo que ninguém mais vai ver. Aceita receptor nil.
func (r *RestoreResult) DiscardLog() {
	if r != nil && r.LogPath != "" {
		_ = os.Remove(r.LogPath)
	}
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
	//
	// Se devolve erro, inclusive por cancelamento do ctx, não resta container
	// nem outro recurso externo criado por ela; ctx cancelado no meio resulta
	// em erro, nunca numa Session parcialmente pronta.
	Provision(ctx context.Context, b *Backup, opts ProvisionOpts) (Session, error)
}

// RecentLimit é o número máximo de linhas que Session.Recent devolve. Engines
// e TUI usam esta constante em vez de repetir o literal.
const RecentLimit = 20

// Session é a conexão viva com o backup restaurado.
type Session interface {
	Health(ctx context.Context) (*Health, error)
	Collections(ctx context.Context, exact bool) ([]Collection, error)
	// Recent devolve no máximo RecentLimit linhas da coleção, as mais
	// recentes primeiro (ordem decrescente) quando a Collection tem coluna
	// de ordenação por data. Sem ela, a ordem é a do fallback da engine
	// (PK, _id, nome de chave…), descrito em Collection.Hint.
	Recent(ctx context.Context, c Collection) (*ResultSet, error)
	Query(ctx context.Context, raw string) (*ResultSet, error)
	ConnectHint() ConnectHint
	// Restore devolve o resultado do restore feito dentro de Provision.
	Restore() *RestoreResult
	Close() error
}

// ------------------------------------------------------------- registry ---

var registry []Engine

// Register cadastra uma engine. Chamado pela lista explícita em engines.go (pacote main).
//
// Regras: Name() é único no registro (Lookup e as fixtures de conformidade
// indexam por nome), então um nome repetido entra em pânico na inicialização
// em vez de passar calado. A ordem de registro só desempata a detecção como
// último recurso: TestNoDetectTies (raiz) falha se duas engines devolverem a
// mesma confiança máxima para alguma fixture de testdata/headers/.
func Register(e Engine) {
	for _, r := range registry {
		if r.Name() == e.Name() {
			panic(fmt.Sprintf("engine.Register: nome duplicado %q", e.Name()))
		}
	}
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
