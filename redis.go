package main

// Implementação da engine Redis atrás da interface Engine/Session. É a
// engine que mais tensiona o modelo genérico (ver ticket 07): não existe
// comando de restore, não existe "20 mais recentes", e "coleção" não é uma
// entidade que o servidor conhece — é inferida por convenção de nome de
// chave. Onde os outros arquivos de engine têm um Restore() que roda um
// cliente contra um servidor já de pé, aqui o próprio ato de subir o
// container *é* o restore: o `.rdb` precisa estar no datadir **antes** do
// `redis-server` iniciar, porque carregar o RDB acontece só na inicialização
// (SPEC.md, "Por engine").
//
// Isso também muda o que "falha de restore" significa: um RDB inválido não
// produz uma mensagem de erro de um cliente, produz um container que morre
// sozinho segundos depois de subir. Provision trata esse caso como as
// demais engines tratam erros de restore parciais — devolve uma Session
// normal, com RestoreResult.Errors preenchido e o log do container em
// arquivo — em vez de propagar um erro de Provision, que faria main.go (e a
// suíte de conformidade) tratar isso como falha genérica de provisionamento
// em vez de "o backup está corrompido".

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func init() { Register(redisEngine{}) }

// redisEngine implementa Engine para Redis.
type redisEngine struct{}

func (redisEngine) Name() string { return "redis" }

// reRDBMagic reconhece o cabeçalho de um RDB: "REDIS" seguido de 4 dígitos
// com a versão do *formato* do RDB (não a versão do servidor — ver
// rdbVersionToRedisTag, abaixo).
var reRDBMagic = regexp.MustCompile(`^REDIS(\d{4})`)

// Detect reconhece um dump.rdb pelo magic "REDIS" + versão do formato, com
// fallback de confiança média para a extensão .rdb — o cabeçalho já chega
// aqui descomprimido (detect.go), então um .rdb.gz é reconhecido igual a um
// .rdb puro.
func (redisEngine) Detect(head []byte, path string) (Match, bool) {
	if mm := reRDBMagic.FindSubmatch(head); mm != nil {
		return Match{Format: "rdb", Version: string(mm[1]), Confidence: ConfidenceMagic}, true
	}
	if strings.HasSuffix(strings.ToLower(path), ".rdb") {
		return Match{Format: "rdb", Confidence: ConfidenceExtension}, true
	}
	return Match{}, false
}

// Expects descreve o que o Redis reconhece, para mensagens de erro e
// --list-engines.
func (redisEngine) Expects() string {
	return `dumps .rdb: magic "REDIS" + versão do formato, ou extensão .rdb`
}

// rdbVersionToRedisTag mapeia a versão do *formato* RDB (o número que segue
// "REDIS" no magic, ex.: RDB versão 11 para "REDIS0011") para uma tag de
// imagem redis:<v>-alpine capaz de carregá-lo. É melhor esforço: o RDB não
// carrega a versão do servidor que o gerou, só a versão do formato, e a
// correspondência entre as duas é histórico público do projeto Redis (o
// campo RDB_VERSION em rdb.h), não algo que o arquivo declare diretamente.
// Uma versão de formato ausente da tabela (mais nova que a última mapeada,
// ou o cabeçalho não foi reconhecido) cai no fallback documentado
// (defaultRedisVersion) — a mesma tag da entrada mais recente conhecida,
// para favorecer compatibilidade com formatos futuros.
var rdbVersionToRedisTag = map[string]string{
	"0006": "2.8",
	"0007": "3.2",
	"0008": "4.0",
	"0009": "4.0",
	"0010": "5.0",
	"0011": "6.2",
	"0012": "7.4",
}

// defaultRedisVersion é o fallback documentado (ticket 07) quando a versão
// do RDB não foi extraída ou não está na tabela conhecida.
const defaultRedisVersion = "7.4"

// redisResolveVersion decide a tag da imagem: --version-tag explícito
// primeiro, depois a versão do RDB traduzida por rdbVersionToRedisTag, e só
// então o fallback. Extraída à parte de Provision para ser testável sem
// Docker (mesmo padrão de resolveMySQLFamilyVersion, mysql.go).
func redisResolveVersion(versionTag, rdbVersion string) string {
	if versionTag != "" {
		return versionTag
	}
	if tag, ok := rdbVersionToRedisTag[rdbVersion]; ok {
		return tag
	}
	return defaultRedisVersion
}

const redisDefaultPort = 6379

// Provision cria o container (sem subir), posiciona o RDB no datadir,
// só então sobe o servidor e espera ele ficar pronto. Deliberadamente fora
// de ordem em relação às outras engines (Start/CopyDump/Restore) porque o
// Redis só carrega o RDB durante a inicialização — colocar o arquivo depois
// do start não faz efeito nenhum.
func (redisEngine) Provision(ctx context.Context, b *Backup, opts ProvisionOpts) (Session, error) {
	if err := dockerAvailable(); err != nil {
		return nil, err
	}

	version := redisResolveVersion(opts.VersionTag, b.Version)
	port := opts.Port
	if port == 0 {
		port = freePortFrom(redisDefaultPort)
	}

	cont := &redisContainer{
		Name:  fmt.Sprintf("db-verify-%d", os.Getpid()),
		Image: "redis:" + version + "-alpine",
		Port:  port,
	}

	opts.report("criando container %s (imagem %s)…", cont.Name, cont.Image)
	if err := cont.Create(ctx); err != nil {
		return nil, err
	}
	opts.report("posicionando o RDB no datadir (antes do servidor subir)…")
	if err := cont.CopyDump(ctx, b); err != nil {
		cont.Remove()
		return nil, err
	}
	opts.report("subindo o Redis…")
	if err := cont.StartContainer(ctx); err != nil {
		cont.Remove()
		return nil, err
	}
	opts.report("aguardando o Redis carregar o RDB…")
	res := cont.WaitReady(ctx, 30*time.Second)

	if len(res.Errors) > 0 {
		// RDB inválido (ou o servidor morreu por outro motivo fatal de
		// inicialização): não há servidor para conectar, mas a Session ainda
		// é devolvida — res já carrega os erros e o log do container, e é
		// isso que main.go (e a suíte de conformidade) inspeciona via
		// Restore(). O container correspondente já está parado/removível;
		// Close() (chamado pelo defer de main.go) cuida da remoção.
		return &redisSession{cont: cont, restore: res}, nil
	}

	client := redisConnect(cont.Addr())
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		cont.Remove()
		return nil, fmt.Errorf("conexão falhou: %w", err)
	}
	return &redisSession{client: client, cont: cont, restore: res}, nil
}

// ---------------------------------------------------------- container ---

// redisContainer representa o container temporário usado para validar o
// RDB. Sem usuário/senha: é um servidor descartável e efêmero, autenticação
// só adicionaria fricção ao --keep sem nenhum benefício de segurança real
// (o container só escuta em 127.0.0.1).
type redisContainer struct {
	Name  string
	Image string
	Port  int
}

func (c *redisContainer) Addr() string { return fmt.Sprintf("127.0.0.1:%d", c.Port) }

// Create sobe o esqueleto do container (docker create, não docker run): ele
// só existe no estado "criado", sem o processo redis-server rodando, para
// CopyDump poder posicionar o RDB no datadir antes de qualquer tentativa de
// carregá-lo. --save "" desliga snapshots automáticos (o container é
// descartável; não precisamos que ele sobrescreva o RDB que acabamos de
// restaurar) sem impedir o carregamento do RDB existente na inicialização,
// que independe de --save.
func (c *redisContainer) Create(ctx context.Context) error {
	args := []string{
		"create", "--name", c.Name,
		"-p", fmt.Sprintf("127.0.0.1:%d:6379", c.Port),
		c.Image,
		"redis-server", "--save", "", "--appendonly", "no",
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("falha ao criar container: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *redisContainer) StartContainer(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "docker", "start", c.Name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("falha ao subir container: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// CopyDump posiciona o backup como /data/dump.rdb (dir e nome default do
// Redis) num container que ainda não subiu. Isso descarta a técnica das
// demais engines (`docker exec ... sh -c "cat > arquivo"`, docker.go/
// postgres.go/mysql.go): não há processo para executar num container
// parado. Em vez disso, `docker cp -` aceita um stream tar via stdin mesmo
// com o container parado. Para o caso comum (sem compressão) isso é feito
// por streaming puro, sem tocar disco no host; um RDB comprimido precisa
// primeiro do tamanho final descomprimido (o cabeçalho do tar exige o
// tamanho antes do corpo), o que só é conhecido depois de descomprimir —
// por isso, só nesse caso, passa por um arquivo temporário local (removido
// ao final). RDBs tendem a ser bem menores que dumps relacionais
// equivalentes, o que torna essa exceção aceitável.
func (c *redisContainer) CopyDump(ctx context.Context, b *Backup) error {
	if b.Compression == "none" {
		return c.streamDumpTar(ctx, b.Path, b.Size)
	}

	r, _, err := openMaybeCompressed(b.Path)
	if err != nil {
		return err
	}
	defer r.Close()

	tmp, err := os.CreateTemp("", "db-verify-redis-*.rdb")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	n, err := io.Copy(tmp, r)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("falha ao descomprimir dump: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return c.streamDumpTar(ctx, tmp.Name(), n)
}

// streamDumpTar envia o conteúdo de path (com o tamanho já conhecido, size)
// como um único arquivo "dump.rdb" dentro de um stream tar, via stdin de
// `docker cp -`.
func (c *redisContainer) streamDumpTar(ctx context.Context, path string, size int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	cmd := exec.CommandContext(ctx, "docker", "cp", "-", c.Name+":/data/")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	tw := tar.NewWriter(stdin)
	writeErr := func() error {
		if err := tw.WriteHeader(&tar.Header{Name: "dump.rdb", Mode: 0o644, Size: size}); err != nil {
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		return tw.Close()
	}()
	stdin.Close()
	waitErr := cmd.Wait()
	if waitErr != nil {
		return fmt.Errorf("docker cp falhou: %s", strings.TrimSpace(stderr.String()))
	}
	return writeErr
}

// WaitReady espera o servidor responder, mas sem esperar cegamente até o
// timeout: se o container morrer sozinho no meio do caminho (o sintoma de
// um RDB inválido — ver testes de conformidade), a falha é detectada e
// reportada com o log do container assim que isso acontece, não só depois
// do timeout inteiro se esgotar. Devolve sempre um *RestoreResult, nunca um
// erro — mesmo a falha de "não ficou pronto" é modelada como um restore com
// erros (ver Provision), não como falha de Provision.
func (c *redisContainer) WaitReady(ctx context.Context, timeout time.Duration) *RestoreResult {
	start := time.Now()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if exec.CommandContext(ctx, "docker", "exec", c.Name, "redis-cli", "PING").Run() == nil {
			return &RestoreResult{Duration: time.Since(start)}
		}
		if !c.isRunning() {
			return c.failureResult(start, "o container saiu sozinho antes de o Redis ficar pronto — RDB provavelmente inválido")
		}
		select {
		case <-ctx.Done():
			return &RestoreResult{Duration: time.Since(start), Errors: []string{ctx.Err().Error()}}
		case <-time.After(300 * time.Millisecond):
		}
	}
	return c.failureResult(start, "timeout esperando o Redis ficar pronto")
}

func (c *redisContainer) isRunning() bool {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", c.Name).Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// failureResult monta o RestoreResult de uma falha de inicialização,
// sempre com o log completo do container salvo em arquivo — é esse log,
// não uma mensagem genérica de timeout, que diagnostica um RDB corrompido.
func (c *redisContainer) failureResult(start time.Time, summary string) *RestoreResult {
	logs, _ := exec.Command("docker", "logs", c.Name).CombinedOutput()
	res := &RestoreResult{
		Duration: time.Since(start),
		Errors:   []string{summary},
	}
	if out, err := exec.Command("docker", "inspect", "-f", "{{.State.ExitCode}}", c.Name).Output(); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
			res.ExitCode = n
		}
	}
	if f, err := os.CreateTemp("", "db-verify-*.log"); err == nil {
		_, _ = f.Write(logs)
		f.Close()
		res.LogPath = f.Name()
	}
	return res
}

func (c *redisContainer) Remove() {
	_ = exec.Command("docker", "rm", "-f", c.Name).Run()
}

// ------------------------------------------------------------- sessão ---

func redisConnect(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: addr, DB: 0})
}

// redisGroupSeparator é o separador usado para inferir grupos de chaves por
// prefixo (ticket 07): tudo antes da primeira ocorrência.
const redisGroupSeparator = ":"

// redisNoPrefixGroup é o nome do grupo reservado para chaves sem nenhuma
// ocorrência do separador — para elas não sumirem da listagem (ticket 07).
const redisNoPrefixGroup = "(sem separador)"

const (
	redisScanCount = int64(500) // COUNT sugerido a cada chamada de SCAN

	// redisExactScanLimit é o teto de segurança da varredura mesmo em modo
	// "exato" (Collections(ctx, true) / Health): acima disso os números
	// viram estimativa em vez de a TUI travar num keyspace de milhões de
	// chaves (SPEC.md, "listar um keyspace de milhões de chaves não é
	// opção").
	redisExactScanLimit = int64(200_000)

	// redisSampleScanLimit é o teto usado em modo estimado
	// (Collections(ctx, false), equivalente a --no-counts): amostra pequena
	// e barata, extrapolada por DBSIZE.
	redisSampleScanLimit = int64(2_000)

	redisRecentSampleSize = 20   // linhas mostradas no painel direito
	redisSampleGatherCap  = 5000 // teto de chaves examinadas ao montar a amostra de UM grupo
)

// redisGroupOf devolve o grupo de uma chave: tudo antes do primeiro
// separador, ou redisNoPrefixGroup quando não há separador.
func redisGroupOf(key string) string {
	if idx := strings.Index(key, redisGroupSeparator); idx >= 0 {
		return key[:idx]
	}
	return redisNoPrefixGroup
}

// redisGroupPattern devolve o padrão de MATCH usado para varrer um grupo e
// o comando SCAN nativo correspondente, copiável para o redis-cli — a
// mesma string usada tanto no Preview (Collections) quanto no Query
// (Recent), para o que aparece na tela ser exatamente o que roda.
func redisGroupPattern(group string) (pattern, nativeCmd string) {
	if group == redisNoPrefixGroup {
		return "*", fmt.Sprintf(`SCAN 0 MATCH "*" COUNT %d`, redisScanCount)
	}
	pattern = group + redisGroupSeparator + "*"
	return pattern, fmt.Sprintf(`SCAN 0 MATCH "%s" COUNT %d`, pattern, redisScanCount)
}

// redisDescriptor é o Descriptor opaco que Collections anexa a cada
// Collection e que Recent usa para montar a varredura — só a engine Redis
// sabe o que esse campo significa.
type redisDescriptor struct {
	Group string
}

type redisSession struct {
	client  *redis.Client
	cont    *redisContainer
	restore *RestoreResult
}

// errNoConn descreve por que a sessão não tem conexão: o restore (a
// inicialização do servidor) falhou, e os detalhes estão em Restore().
func (s *redisSession) errNoConn() error {
	n := 0
	if s.restore != nil {
		n = len(s.restore.Errors)
	}
	return fmt.Errorf("sem conexão: o restore falhou com %d erro(s) — veja Restore() e o log do container", n)
}

// redisKeyspaceScan é o resultado de uma varredura do keyspace: contagem por
// grupo (prefixo), por tipo, quantas chaves têm TTL, e se a varredura foi
// interrompida pelo teto de segurança antes de esgotar o cursor (nesse caso
// os números são amostra, não exatos).
type redisKeyspaceScan struct {
	Groups     map[string]int64
	TypeCounts map[string]int64
	WithTTL    int64
	Scanned    int64
	Truncated  bool
}

// redisKeyMeta é o TYPE e o TTL de uma chave, pareados por índice com a
// lista de chaves que os produziu.
type redisKeyMeta struct {
	Type string
	TTL  time.Duration
}

// fetchKeyMeta busca TYPE e TTL de um lote de chaves com uma única
// ida-e-volta (Pipeline), não uma por chave — usado tanto por scanKeyspace
// (contagem por tipo/TTL do keyspace inteiro) quanto por sampleGroup (o
// mesmo par de informações para a amostra exibida no painel direito), para
// um keyspace grande não travar a TUI em nenhum dos dois casos.
func (s *redisSession) fetchKeyMeta(ctx context.Context, keys []string) ([]redisKeyMeta, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	pipe := s.client.Pipeline()
	typeCmds := make([]*redis.StatusCmd, len(keys))
	ttlCmds := make([]*redis.DurationCmd, len(keys))
	for i, k := range keys {
		typeCmds[i] = pipe.Type(ctx, k)
		ttlCmds[i] = pipe.TTL(ctx, k)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}
	meta := make([]redisKeyMeta, len(keys))
	for i := range keys {
		typ, _ := typeCmds[i].Result()
		ttl, _ := ttlCmds[i].Result()
		meta[i] = redisKeyMeta{Type: typ, TTL: ttl}
	}
	return meta, nil
}

// scanKeyspace varre até limit chaves via SCAN (nunca o keyspace inteiro de
// uma vez), buscando TYPE/TTL em lote (fetchKeyMeta) — uma ida-e-volta por
// lote de até redisScanCount chaves, não uma por chave, para um keyspace
// grande não travar a TUI.
func (s *redisSession) scanKeyspace(ctx context.Context, limit int64) (*redisKeyspaceScan, error) {
	scan := &redisKeyspaceScan{Groups: map[string]int64{}, TypeCounts: map[string]int64{}}
	var cursor uint64
	for {
		keys, next, err := s.client.Scan(ctx, cursor, "*", redisScanCount).Result()
		if err != nil {
			return nil, err
		}
		meta, err := s.fetchKeyMeta(ctx, keys)
		if err != nil {
			return nil, err
		}
		for i, k := range keys {
			scan.Scanned++
			scan.Groups[redisGroupOf(k)]++
			if meta[i].Type != "" {
				scan.TypeCounts[meta[i].Type]++
			}
			if meta[i].TTL > 0 {
				scan.WithTTL++
			}
		}
		cursor = next
		if cursor == 0 || scan.Scanned >= limit {
			scan.Truncated = cursor != 0
			break
		}
	}
	return scan, nil
}

// redisHealthTypes é a ordem fixa de exibição dos tipos de dado do Redis no
// cabeçalho — só esses seis existem no protocolo (TYPE nunca devolve outra
// coisa para uma chave existente).
var redisHealthTypes = []string{"string", "hash", "list", "set", "zset", "stream"}

func (s *redisSession) Health(ctx context.Context) (*Health, error) {
	if s.client == nil {
		return nil, s.errNoConn()
	}
	dbSize, err := s.client.DBSize(ctx).Result()
	if err != nil {
		return nil, err
	}
	info, err := s.client.Info(ctx, "memory").Result()
	if err != nil {
		return nil, err
	}
	used := parseRedisInfoField(info, "used_memory_human")
	if used == "" {
		used = "?"
	}

	scan, err := s.scanKeyspace(ctx, redisExactScanLimit)
	if err != nil {
		return nil, err
	}

	fields := []HealthField{{"chaves", fmt.Sprint(dbSize)}} // DBSIZE é sempre exato no Redis (SPEC.md)
	for _, typ := range redisHealthTypes {
		fields = append(fields, HealthField{typ, fmt.Sprint(scan.TypeCounts[typ])})
	}
	fields = append(fields, HealthField{"com TTL", fmt.Sprint(scan.WithTTL)})
	if scan.Truncated {
		fields = append(fields, HealthField{"amostragem", fmt.Sprintf("parcial (%d/%d chaves)", scan.Scanned, dbSize)})
	}

	return &Health{Name: "db0", Size: used, Fields: fields}, nil
}

// parseRedisInfoField lê um campo "chave:valor\r\n" da saída de INFO.
func parseRedisInfoField(info, field string) string {
	for _, line := range strings.Split(info, "\r\n") {
		if name, val, ok := strings.Cut(line, ":"); ok && name == field {
			return val
		}
	}
	return ""
}

// Collections agrupa as chaves por prefixo (ticket 07). exact=true varre até
// redisExactScanLimit chaves (contagem exata dentro desse teto); exact=false
// varre só redisSampleScanLimit e extrapola por DBSIZE — o equivalente do
// Redis a "contagem estimada" (--no-counts), já que não existe um
// n_live_tup/TABLE_ROWS para grupos que não são uma entidade do servidor.
func (s *redisSession) Collections(ctx context.Context, exact bool) ([]Collection, error) {
	if s.client == nil {
		return nil, s.errNoConn()
	}
	limit := redisSampleScanLimit
	if exact {
		limit = redisExactScanLimit
	}
	scan, err := s.scanKeyspace(ctx, limit)
	if err != nil {
		return nil, err
	}
	dbSize, err := s.client.DBSize(ctx).Result()
	if err != nil {
		return nil, err
	}

	var out []Collection
	for group, count := range scan.Groups {
		n := count
		if scan.Truncated && scan.Scanned > 0 {
			n = int64(float64(count) / float64(scan.Scanned) * float64(dbSize))
		}
		_, nativeCmd := redisGroupPattern(group)
		out = append(out, Collection{
			Name:       group,
			Count:      n,
			Hint:       redisHint(scan.Truncated),
			Preview:    nativeCmd,
			Descriptor: redisDescriptor{Group: group},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func redisHint(truncated bool) string {
	if truncated {
		return "contagem estimada por amostragem (SCAN)"
	}
	return "contagem exata (SCAN completo)"
}

// Recent devolve uma amostra de chaves do grupo — "mais recente" não existe
// no Redis (ticket 07). A amostra é ordenada por nome de chave em ordem
// decrescente antes de aplicar o limite de 20: determinístico, e alinhado
// com a convenção real mais comum de nomear chaves com um sufixo crescente
// (id, timestamp) — a chave "maior" tende a ser a mais nova.
func (s *redisSession) Recent(ctx context.Context, c Collection) (*ResultSet, error) {
	if s.client == nil {
		return nil, s.errNoConn()
	}
	d, _ := c.Descriptor.(redisDescriptor)
	return s.sampleGroup(ctx, d.Group)
}

func (s *redisSession) sampleGroup(ctx context.Context, group string) (*ResultSet, error) {
	start := time.Now()
	pattern, nativeCmd := redisGroupPattern(group)
	noPrefix := group == redisNoPrefixGroup

	var keys []string
	var cursor uint64
	examined := 0
	for {
		batch, next, err := s.client.Scan(ctx, cursor, pattern, redisScanCount).Result()
		if err != nil {
			return nil, err
		}
		for _, k := range batch {
			examined++
			// SCAN com MATCH "*" (grupo sem separador) não sabe filtrar por
			// "ausência de separador" — o filtro é feito aqui.
			if noPrefix && strings.Contains(k, redisGroupSeparator) {
				continue
			}
			keys = append(keys, k)
		}
		cursor = next
		if cursor == 0 || examined >= redisSampleGatherCap {
			break
		}
	}

	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	if len(keys) > redisRecentSampleSize {
		keys = keys[:redisRecentSampleSize]
	}

	rs := &ResultSet{Columns: []string{"chave", "tipo", "ttl", "preview"}, Query: nativeCmd, Language: "redis"}
	meta, err := s.fetchKeyMeta(ctx, keys)
	if err != nil {
		return nil, err
	}
	for i, k := range keys {
		rs.Rows = append(rs.Rows, []string{k, meta[i].Type, redisFormatTTL(meta[i].TTL), s.previewValue(ctx, k, meta[i].Type)})
	}
	rs.Elapsed = time.Since(start)
	return rs, nil
}

// redisFormatTTL formata a duração devolvida por TTL: negativa tanto para
// -1 (sem expiração) quanto -2 (chave sumiu entre o SCAN e a leitura —
// condição de corrida, rara mas possível) — os dois viram texto explícito
// em vez do número negativo cru.
func redisFormatTTL(ttl time.Duration) string {
	if ttl < 0 {
		return "sem TTL"
	}
	return ttl.Round(time.Second).String()
}

const redisPreviewMaxLen = 60

// previewValue busca um preview curto do valor, no formato certo para o
// tipo — erros aqui viram "?" em vez de propagados: é só um preview, uma
// chave que sumiu entre o SCAN e a leitura (TTL expirou, outro processo
// deletou) não deveria derrubar a listagem inteira.
func (s *redisSession) previewValue(ctx context.Context, key, typ string) string {
	switch typ {
	case "string":
		v, err := s.client.Get(ctx, key).Result()
		if err != nil {
			return "?"
		}
		return truncate(strings.Join(strings.Fields(v), " "), redisPreviewMaxLen)
	case "hash":
		n, _ := s.client.HLen(ctx, key).Result()
		if n > 20 {
			return fmt.Sprintf("hash com %d campos", n)
		}
		m, err := s.client.HGetAll(ctx, key).Result()
		if err != nil {
			return "?"
		}
		return truncate(formatMap(m), redisPreviewMaxLen)
	case "list":
		n, _ := s.client.LLen(ctx, key).Result()
		vs, err := s.client.LRange(ctx, key, 0, 2).Result()
		if err != nil {
			return "?"
		}
		return truncate(fmt.Sprintf("[%d] %s", n, strings.Join(vs, ", ")), redisPreviewMaxLen)
	case "set":
		n, _ := s.client.SCard(ctx, key).Result()
		vs, err := s.client.SRandMemberN(ctx, key, 3).Result()
		if err != nil {
			return "?"
		}
		return truncate(fmt.Sprintf("{%d} %s", n, strings.Join(vs, ", ")), redisPreviewMaxLen)
	case "zset":
		n, _ := s.client.ZCard(ctx, key).Result()
		vs, err := s.client.ZRangeWithScores(ctx, key, 0, 2).Result()
		if err != nil {
			return "?"
		}
		parts := make([]string, len(vs))
		for i, z := range vs {
			parts[i] = fmt.Sprintf("%v:%g", z.Member, z.Score)
		}
		return truncate(fmt.Sprintf("<%d> %s", n, strings.Join(parts, ", ")), redisPreviewMaxLen)
	case "stream":
		n, err := s.client.XLen(ctx, key).Result()
		if err != nil {
			return "?"
		}
		return fmt.Sprintf("%d entradas", n)
	default:
		return "?"
	}
}

func formatMap(m map[string]string) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// splitRedisCmd tokeniza um comando nativo digitado como texto — separado
// por espaço, com suporte a aspas duplas para argumentos com espaço dentro
// (ex.: SET chave "valor com espaço"). Suficiente para Query, que só recebe
// comandos de teste/depuração, não um parser RESP completo.
func splitRedisCmd(raw string) []string {
	var args []string
	var cur strings.Builder
	inQuotes := false
	for _, r := range raw {
		switch {
		case r == '"':
			inQuotes = !inQuotes
		case (r == ' ' || r == '\t') && !inQuotes:
			if cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		args = append(args, cur.String())
	}
	return args
}

// Query roda um comando nativo arbitrário via client.Do — a mesma interface
// genérica que a suíte de conformidade usa para "comando válido"/"comando
// inválido" (ver redis_conformance_test.go). Não há UI para digitar
// consulta livre nesta entrega (SPEC.md, "Fora do escopo"); a interface
// existe e é usada internamente e pelos testes.
func (s *redisSession) Query(ctx context.Context, raw string) (*ResultSet, error) {
	if s.client == nil {
		return nil, s.errNoConn()
	}
	args := splitRedisCmd(raw)
	if len(args) == 0 {
		return nil, fmt.Errorf("comando vazio")
	}
	iargs := make([]any, len(args))
	for i, a := range args {
		iargs[i] = a
	}

	start := time.Now()
	res, err := s.client.Do(ctx, iargs...).Result()
	if err != nil {
		return nil, err
	}
	rs := &ResultSet{Query: raw, Language: "redis", Columns: []string{"resultado"}, Elapsed: time.Since(start)}
	for _, line := range redisFormatResult(res) {
		rs.Rows = append(rs.Rows, []string{line})
	}
	return rs, nil
}

// redisFormatResult achata o retorno solto (any) do client.Do em linhas de
// texto — RESP pode devolver string, inteiro, nil, ou uma lista, e o
// resultado precisa virar texto legível de um jeito ou de outro, igual
// formatValue (postgres.go) faz para os tipos de coluna SQL.
func redisFormatResult(v any) []string {
	switch x := v.(type) {
	case nil:
		return []string{"∅"}
	case []any:
		if len(x) == 0 {
			return []string{"(vazio)"}
		}
		var out []string
		for _, e := range x {
			out = append(out, fmt.Sprint(e))
		}
		return out
	default:
		return []string{fmt.Sprint(x)}
	}
}

func (s *redisSession) ConnectHint() ConnectHint {
	return ConnectHint{
		Name:      s.cont.Name,
		DSN:       fmt.Sprintf("redis://%s/0", s.cont.Addr()),
		Shell:     fmt.Sprintf("redis-cli -p %d", s.cont.Port),
		ExecShell: fmt.Sprintf("docker exec -it %s redis-cli", s.cont.Name),
		Remove:    fmt.Sprintf("docker rm -f %s", s.cont.Name),
		Port:      s.cont.Port,
	}
}

func (s *redisSession) Restore() *RestoreResult { return s.restore }

func (s *redisSession) Close() error {
	if s.client != nil {
		s.client.Close()
	}
	s.cont.Remove()
	return nil
}
