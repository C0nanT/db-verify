package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
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

// maxPortRetries é quantas portas seguidas startWithPortRetry tenta antes de
// desistir e oferecer ao operador para derrubar um container. freePort/
// freePortFrom já evitam a maioria dos conflitos verificando o host antes de
// subir o container, mas ainda existe uma janela de corrida entre esse
// check e o docker de fato publicar a porta — e o operador pode ter passado
// --port explicitamente, pulando o scan. startWithPortRetry cobre os dois
// casos.
const maxPortRetries = 5

// isPortConflict reconhece, na mensagem de erro do docker, que a falha foi
// por a porta já estar em uso (por outro container ou processo) — para
// distinguir de qualquer outra falha de start, que não deve ser retentada.
func isPortConflict(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, "port is already allocated") ||
		strings.Contains(low, "address already in use") ||
		(strings.Contains(low, "bind for") && strings.Contains(low, "failed"))
}

// startWithPortRetry chama attempt(port), começando em startPort. Quando
// attempt falha por conflito de porta, remove o que sobrou do container
// (nome fixo por processo — precisa estar livre para a próxima tentativa
// reusar) e tenta de novo na porta seguinte, até maxPortRetries vezes.
// Qualquer falha que não seja conflito de porta volta na hora, sem retry.
//
// Se as tentativas se esgotarem, oferece ao operador (via offerToFreePort)
// derrubar um dos containers docker que já estão publicando portas nessa
// faixa; se ele derrubar um, tenta a faixa inteira de novo.
func startWithPortRetry(ctx context.Context, name string, startPort int, attempt func(port int) error) (int, error) {
	port := startPort
	var lastErr error
	tried := make([]int, 0, maxPortRetries)
	for i := 0; i < maxPortRetries; i++ {
		tried = append(tried, port)
		err := attempt(port)
		if err == nil {
			return port, nil
		}
		if !isPortConflict(err.Error()) {
			return 0, err
		}
		lastErr = err
		exec.CommandContext(ctx, "docker", "rm", "-f", name).Run() //nolint:errcheck // best-effort; pode nem ter chegado a existir
		fmt.Fprintf(os.Stderr, "%s porta %d já em uso, tentando %d…\n", stWarn.Render("!"), port, port+1)
		port++
	}

	freed, err := offerToFreePort(ctx, tried, lastErr)
	if err != nil {
		return 0, err
	}
	if !freed {
		return 0, fmt.Errorf("nenhuma porta livre entre %d e %d: %w", tried[0], tried[len(tried)-1], lastErr)
	}
	return startWithPortRetry(ctx, name, startPort, attempt)
}

// portUser é uma linha de "docker ps" filtrada por publicar uma das portas
// que startWithPortRetry tentou e não conseguiu.
type portUser struct {
	Name  string
	Image string
	Ports string
}

// containersUsingPorts lista, sem duplicar, os containers docker que
// publicam qualquer uma das portas em ports.
func containersUsingPorts(ctx context.Context, ports []int) ([]portUser, error) {
	seen := map[string]portUser{}
	for _, p := range ports {
		out, err := exec.CommandContext(ctx, "docker", "ps",
			"--filter", fmt.Sprintf("publish=%d", p),
			"--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Ports}}").Output()
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			f := strings.SplitN(line, "\t", 4)
			if len(f) != 4 {
				continue
			}
			seen[f[0]] = portUser{Name: f[1], Image: f[2], Ports: f[3]}
		}
	}
	out := make([]portUser, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// offerToFreePort lista os containers docker ocupando as portas em tried e
// pergunta ao operador, no terminal, se algum deles pode ser derrubado.
// Devolve true quando um container foi derrubado (a chamada deve tentar de
// novo); false quando o operador cancelou ou não há nada pra listar (a
// chamada deve reportar cause como o erro final).
func offerToFreePort(ctx context.Context, tried []int, cause error) (bool, error) {
	containers, err := containersUsingPorts(ctx, tried)
	if err != nil || len(containers) == 0 {
		return false, nil
	}

	fmt.Println()
	fmt.Printf("%s %d tentativas de porta (%d–%d) falharam; containers docker publicando portas nessa faixa:\n",
		stWarn.Render("!"), len(tried), tried[0], tried[len(tried)-1])
	for i, c := range containers {
		fmt.Printf("  [%d] %-30s %-25s %s\n", i+1, c.Name, c.Image, c.Ports)
	}
	fmt.Print("número do container para derrubar (enter cancela): ")

	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return false, nil
	}
	idx, convErr := strconv.Atoi(line)
	if convErr != nil || idx < 1 || idx > len(containers) {
		return false, fmt.Errorf("opção inválida: %q", line)
	}
	chosen := containers[idx-1]
	fmt.Printf("derrubando %s…\n", chosen.Name)
	if out, err := exec.CommandContext(ctx, "docker", "rm", "-f", chosen.Name).CombinedOutput(); err != nil {
		return false, fmt.Errorf("falha ao derrubar %s: %s", chosen.Name, strings.TrimSpace(string(out)))
	}
	return true, nil
}
