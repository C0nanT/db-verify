package main

// Implementação da engine MongoDB atrás da interface Engine/Session. É a
// engine que mais tensiona o modelo de Collection/ResultSet do jeito
// contrário do Redis (ticket 07): lá não havia "coleção" nem "recente" de
// verdade; aqui há as duas, mas os documentos não têm colunas fixas — o
// desafio é caber documentos aninhados, de formato variável, num painel que é
// uma tabela. A solução (ver mongoFlattenInto) é achatar cada documento em
// caminho pontilhado (endereco.cidade) até uma profundidade fixa, tratar
// arrays sempre como JSON compacto (nunca recursar dentro deles), e montar o
// conjunto de colunas como a união dos caminhos vistos na amostra — colunas
// ausentes num documento específico viram "∅", igual valor nulo em qualquer
// outra engine.
//
// O outro tensionamento é o restore: um archive do mongodump pode conter
// várias databases (é um dump do servidor inteiro, não de uma database só),
// diferente do "banco de destino" único que as demais engines assumem via
// opts.DBName. Por isso mongoSession não tem um "banco corrente": Collections
// varre todas as databases restauradas (exceto admin/local/config) e
// Namespace vira o nome de cada uma — "db.coleção", como o SPEC pede.

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

func init() { Register(mongoEngine{}) }

// mongoEngine implementa Engine para MongoDB.
type mongoEngine struct{}

func (mongoEngine) Name() string { return "mongodb" }

// mongoArchiveMagic são os 4 bytes de assinatura de um archive do mongodump
// (mongo-tools, common/archive/archive.go: "const MagicNumber uint32 =
// 0x8199e26d"), lidos como little-endian — ou seja, na ordem em que aparecem
// no arquivo.
var mongoArchiveMagic = []byte{0x6d, 0xe2, 0x99, 0x81}

// reMongoVersion extrai major.minor de uma versão de servidor completa (ex.:
// "7.0.14" -> "7.0") — as tags de imagem oficiais do Mongo no Docker Hub são
// major.minor, não a patch exata.
var reMongoVersion = regexp.MustCompile(`^(\d+\.\d+)`)

// Detect reconhece um archive do mongodump pelos 4 bytes de magic number,
// com fallback de confiança média para a extensão .archive. A versão do
// servidor de origem, quando presente, vem do campo "server_version" do
// cabeçalho BSON que segue o magic number (ver mongoExtractHeaderField).
func (mongoEngine) Detect(head []byte, path string) (Match, bool) {
	if bytes.HasPrefix(head, mongoArchiveMagic) {
		m := Match{Format: "archive", Confidence: ConfidenceMagic}
		if v := mongoExtractHeaderField(head, "server_version"); v != "" {
			if mm := reMongoVersion.FindStringSubmatch(v); mm != nil {
				m.Version = mm[1]
			}
		}
		return m, true
	}
	if strings.HasSuffix(strings.ToLower(path), ".archive") {
		return Match{Format: "archive", Confidence: ConfidenceExtension}, true
	}
	return Match{}, false
}

// Expects descreve o que o MongoDB reconhece, para mensagens de erro e
// --list-engines.
func (mongoEngine) Expects() string {
	return "archives do mongodump --archive: magic number 0x8199e26d, ou extensão .archive"
}

// mongoExtractHeaderField lê o valor de um campo string do documento BSON que
// segue o magic number (mongo-tools, archive.go: type Header, campos
// concurrent_collections/version/server_version/tool_version), sem depender
// de um parser BSON completo — suficiente porque só o cabeçalho (já
// descomprimido, headerSize bytes) chega aqui, e o campo procurado é sempre
// uma string BSON (tipo 0x02): chave + \x00, então int32 little-endian com o
// tamanho (incluindo o \x00 final), então os bytes da string.
func mongoExtractHeaderField(head []byte, field string) string {
	key := []byte(field + "\x00")
	idx := bytes.Index(head, key)
	if idx < 0 {
		return ""
	}
	lenStart := idx + len(key)
	if lenStart+4 > len(head) {
		return ""
	}
	strLen := int(binary.LittleEndian.Uint32(head[lenStart : lenStart+4]))
	dataStart := lenStart + 4
	if strLen < 1 || dataStart+strLen > len(head) {
		return ""
	}
	return string(head[dataStart : dataStart+strLen-1]) // strLen inclui o \x00 final
}

// defaultMongoVersion é o fallback documentado quando o archive não informa
// a versão do servidor de origem: a série estável mais recente no momento
// desta entrega.
const defaultMongoVersion = "7.0"

// resolveMongoVersion decide a tag da imagem: --version-tag explícito
// primeiro, depois a versão extraída do archive (já normalizada para
// major.minor por Detect), e só então o fallback — mesma ordem de precedência
// das demais engines (ver resolveMySQLFamilyVersion, mysql.go).
func resolveMongoVersion(versionTag, backupVersion string) string {
	if versionTag != "" {
		return versionTag
	}
	if backupVersion != "" {
		return backupVersion
	}
	return defaultMongoVersion
}

const mongoDefaultPort = 27017

// mongoSystemDBs são as databases internas do servidor, nunca coleções do
// backup do operador — omitidas de Collections/Health.
var mongoSystemDBs = map[string]bool{"admin": true, "local": true, "config": true}

// Provision sobe o container, espera o mongod ficar pronto, restaura o
// archive via mongorestore lendo de stdin (sem arquivo intermediário dentro
// do container) e conecta — mesmo formato grosso das demais engines.
func (mongoEngine) Provision(ctx context.Context, b *Backup, opts ProvisionOpts) (Session, error) {
	if err := dockerAvailable(); err != nil {
		return nil, err
	}

	version := resolveMongoVersion(opts.VersionTag, b.Version)
	port := opts.Port
	if port == 0 {
		port = freePortFrom(mongoDefaultPort)
	}

	cont := &mongoContainer{
		Name:  fmt.Sprintf("db-verify-%d", os.Getpid()),
		Image: "mongo:" + version,
		Port:  port,
	}

	opts.report("subindo container %s (imagem %s)…", cont.Name, cont.Image)
	if err := cont.Start(ctx); err != nil {
		return nil, err
	}
	// O Mongo demora mais para ficar pronto que as demais engines (ticket
	// 09: "o timeout de 'ficar pronto' é específico do Mongo") — imagem
	// maior e inicialização em duas etapas (bootstrap + servidor real).
	opts.report("aguardando o MongoDB ficar pronto…")
	if err := cont.WaitReady(ctx, 180*time.Second); err != nil {
		cont.Remove()
		return nil, err
	}
	opts.report("restaurando via mongorestore --archive (pode demorar)…")
	res, err := cont.Restore(ctx, b, opts.Jobs)
	if err != nil {
		cont.Remove()
		return nil, err
	}

	client, err := mongoConnect(ctx, cont.URI())
	if err != nil {
		cont.Remove()
		return nil, fmt.Errorf("conexão falhou: %w", err)
	}

	dbNames, err := mongoRestoredDBs(ctx, client)
	if err != nil {
		client.Disconnect(ctx)
		cont.Remove()
		return nil, err
	}

	return &mongoSession{client: client, cont: cont, restore: res, dbNames: dbNames}, nil
}

// mongoRestoredDBs lista as databases que o restore de fato produziu,
// excluindo as internas do servidor (mongoSystemDBs) — o que Collections e
// Health enxergam como o conteúdo do backup.
func mongoRestoredDBs(ctx context.Context, client *mongo.Client) ([]string, error) {
	all, err := client.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, name := range all {
		if !mongoSystemDBs[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// ---------------------------------------------------------- container ---

// mongoContainer representa o container temporário usado para validar o
// archive. Sem usuário/senha: servidor descartável e efêmero, igual à
// escolha do Redis (redis.go) — o container só escuta em 127.0.0.1.
type mongoContainer struct {
	Name  string
	Image string
	Port  int
}

func (c *mongoContainer) URI() string {
	return fmt.Sprintf("mongodb://127.0.0.1:%d", c.Port)
}

// Start sobe o container sem nenhuma flag de tuning: ao contrário do
// Postgres/MySQL (fsync desligado, etc.), o mongod não tem um equivalente
// simples e amplamente compatível entre versões — a inicialização já é a
// parte lenta (daí o timeout maior em WaitReady), não o restore em si.
func (c *mongoContainer) Start(ctx context.Context) error {
	args := []string{
		"run", "-d", "--name", c.Name,
		"-p", fmt.Sprintf("127.0.0.1:%d:27017", c.Port),
		c.Image,
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("falha ao subir container: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// WaitReady espera uma conexão de verdade responder a Ping, via driver (em
// vez de docker exec com um shell CLI, como as demais engines) — o
// mongosh/mongo shell não é garantido em toda tag de imagem, mas o pool de
// conexão que a sessão vai usar depois é.
func (c *mongoContainer) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		client, err := mongo.Connect(options.Client().ApplyURI(c.URI()).SetServerSelectionTimeout(2 * time.Second))
		if err == nil {
			pingErr := client.Ping(ctx, readpref.Primary())
			client.Disconnect(ctx)
			if pingErr == nil {
				return nil
			}
			lastErr = pingErr
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	logs, _ := exec.Command("docker", "logs", "--tail", "20", c.Name).CombinedOutput()
	return fmt.Errorf("timeout esperando o MongoDB (%v):\n%s", lastErr, string(logs))
}

// reMongoRestoreFailedLine reconhece as linhas de erro que o mongorestore
// escreve em stderr, tanto por namespace ("Failed: db.coll: ...") quanto de
// falha geral de leitura do archive.
var reMongoRestoreFailedLine = regexp.MustCompile(`(?im)^\s*Failed:.*$|error restoring from archive`)

// reMongoRestoreSummary lê a linha de resumo final do mongorestore ("N
// document(s) restored successfully. M document(s) failed to restore."),
// usada para detectar falhas parciais que não batem no formato "Failed:"
// acima.
var reMongoRestoreSummary = regexp.MustCompile(`(\d+) document\(s\) failed to restore`)

// Restore executa mongorestore --archive dentro do container, lendo o
// backup (já descomprimido sob demanda) via stdin — nunca grava um arquivo
// intermediário dentro do container (ticket 09).
func (c *mongoContainer) Restore(ctx context.Context, b *Backup, jobs int) (*RestoreResult, error) {
	r, _, err := openMaybeCompressed(b.Path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	if jobs <= 0 {
		jobs = 4
	}
	args := []string{"exec", "-i", c.Name, "mongorestore", "--archive",
		fmt.Sprintf("--numParallelCollections=%d", jobs)}

	start := time.Now()
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = r
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()

	res := &RestoreResult{Duration: time.Since(start)}
	if ee, ok := runErr.(*exec.ExitError); ok {
		res.ExitCode = ee.ExitCode()
	} else if runErr != nil {
		return nil, runErr
	}

	output := out.String()
	for _, line := range strings.Split(output, "\n") {
		if reMongoRestoreFailedLine.MatchString(line) {
			res.Errors = append(res.Errors, strings.TrimSpace(line))
		}
	}
	if mm := reMongoRestoreSummary.FindStringSubmatch(output); mm != nil && mm[1] != "0" {
		res.Errors = append(res.Errors, fmt.Sprintf("%s documento(s) falharam ao restaurar", mm[1]))
	}
	// Rede de segurança: um exit code != 0 é sempre uma falha de verdade,
	// mesmo que nenhuma das expressões acima tenha casado com o texto exato
	// desta versão do mongorestore — não silenciar o erro (SPEC.md, "erros
	// de restore de cada engine sejam reconhecidos como erros de verdade").
	if res.ExitCode != 0 && len(res.Errors) == 0 {
		res.Errors = append(res.Errors, fmt.Sprintf("mongorestore saiu com código %d", res.ExitCode))
	}
	if len(res.Errors) > 0 {
		if f, e := os.CreateTemp("", "db-verify-*.log"); e == nil {
			f.WriteString(output)
			f.Close()
			res.LogPath = f.Name()
		}
	}
	return res, nil
}

func (c *mongoContainer) Remove() {
	exec.Command("docker", "rm", "-f", c.Name).Run()
}

// ------------------------------------------------------------- sessão ---

func mongoConnect(ctx context.Context, uri string) (*mongo.Client, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		client.Disconnect(ctx)
		return nil, err
	}
	return client, nil
}

// mongoOrderFields é a mesma heurística de nomes de coluna compartilhada
// pelas engines relacionais (orderColumnTiers, relational.go), achatada numa
// lista única em ordem de preferência — o Mongo não tem information_schema
// para inspecionar tipos de coluna sem consultar, então a decisão aqui é
// feita a partir de uma amostra de documento (ver mongoChooseOrderField), não
// de um catálogo.
var mongoOrderFields = append(append(append([]string{},
	orderColumnTiers[0]...), orderColumnTiers[1]...), orderColumnTiers[2]...)

// mongoDescriptor é o Descriptor opaco que Collections anexa a cada
// Collection e que Recent usa para montar a consulta — só a engine MongoDB
// sabe o que este campo significa. OrderField vazio significa "sem campo de
// data reconhecido", e Recent ordena por _id decrescente (ticket 09).
type mongoDescriptor struct {
	OrderField string
}

// mongoChooseOrderField decide o campo de ordenação a partir de UM documento
// de amostra: o primeiro nome da lista compartilhada (mongoOrderFields) cujo
// valor no documento é um bson.DateTime vence; sem nenhum, cai para _id
// decrescente (OrderField == "", ticket 09 — "o ObjectId carrega o timestamp
// de criação, então isso entrega de fato os documentos mais recentes").
func mongoChooseOrderField(sample bson.M) string {
	if sample == nil {
		return ""
	}
	for _, name := range mongoOrderFields {
		if v, ok := sample[name]; ok {
			if _, isDate := v.(bson.DateTime); isDate {
				return name
			}
		}
	}
	return ""
}

// mongoOrderHint descreve, numa linha, como Recent escolheu ordenar uma
// coleção — equivalente ao orderHint compartilhado das engines relacionais
// (relational.go), mas o fallback do Mongo nunca é "sem coluna": há sempre
// _id para ordenar por.
func mongoOrderHint(field string) string {
	if field == "" {
		return "ordenado por _id (mais recente primeiro; sem campo de data)"
	}
	return fmt.Sprintf("ordenado por %s (data)", field)
}

// mongoRecentQuery monta o comando mongosh copiável dos 20 documentos mais
// recentes da coleção — "use <db>;" na frente porque um archive pode conter
// várias databases, e db.<coleção>.find() sozinho assume a corrente.
func mongoRecentQuery(dbName, coll string, d mongoDescriptor) string {
	field := "_id"
	if d.OrderField != "" {
		field = d.OrderField
	}
	return fmt.Sprintf("use %s; db.%s.find().sort({%s: -1}).limit(%d)", dbName, coll, field, mongoRecentLimit)
}

const mongoRecentLimit = 20

// mongoFlattenDepth é a profundidade fixa até onde documentos aninhados são
// achatados em caminho pontilhado (ticket 09) — além dela, o sub-documento
// vira JSON compacto no próprio caminho, igual um array trata (ver
// mongoFlattenInto), em vez de se perder.
const mongoFlattenDepth = 3

// mongoFlattenInto achata doc em out, com chaves de caminho pontilhado
// (endereco.cidade) até depth níveis. Arrays (bson.A) nunca recursam — viram
// JSON compacto no próprio caminho, independentemente da profundidade
// restante, porque um array de sub-documentos heterogêneos não tem uma
// "coluna" única para achatar (ticket 09: "arrays são renderizados como JSON
// compacto"). Sub-documentos além de depth também viram JSON compacto, para
// não perder o dado.
func mongoFlattenInto(doc bson.M, prefix string, depth int, out map[string]string) {
	for k, v := range doc {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		switch x := v.(type) {
		case bson.M:
			if depth > 0 {
				mongoFlattenInto(x, path, depth-1, out)
			} else {
				out[path] = mongoCompactJSON(x)
			}
		case bson.A:
			out[path] = mongoCompactJSON(x)
		default:
			out[path] = mongoFormatScalar(x)
		}
	}
}

// mongoCompactJSON renderiza um valor BSON qualquer (documento, array, ou
// tipo composto) como JSON compacto de uma linha, no modo relaxado (não
// canônico) do extended JSON — legível ("2024-01-01T00:00:00Z" em vez do
// envelope {"$date":...}), mas ainda JSON de verdade. Erros de marshaling
// (não deveriam ocorrer para valores que já vieram de um decode BSON válido)
// viram "?" em vez de propagados, mesmo espírito de redis.go/previewValue:
// é só uma preview, não pode derrubar a listagem inteira.
func mongoCompactJSON(v any) string {
	// MarshalExtJSON só aceita um documento na raiz — um bson.A (array)
	// direto na raiz devolve erro ("positioned on a TopLevel"). O valor é
	// embrulhado num documento de um campo só, e o embrulho removido do
	// texto depois, para arrays e sub-documentos passarem pelo mesmo
	// caminho.
	wrapped, err := bson.MarshalExtJSON(bson.D{{Key: "v", Value: v}}, false, false)
	if err != nil {
		return "?"
	}
	s := strings.TrimSuffix(strings.TrimPrefix(string(wrapped), `{"v":`), "}")
	return strings.Join(strings.Fields(s), " ")
}

// mongoFormatScalar formata um valor-folha (não documento, não array) —
// mesmo espírito de formatValue (postgres.go) e mysqlFormatValue (mysql.go):
// nulo/data/binário/string recebem tratamento explícito, o resto cai no
// %v genérico.
func mongoFormatScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return "∅"
	case bson.DateTime:
		return x.Time().UTC().Format("2006-01-02 15:04:05")
	case bson.ObjectID:
		return x.Hex()
	case bson.Binary:
		return fmt.Sprintf("\\x%x", x.Data)
	case bson.Decimal128:
		return x.String()
	case string:
		return strings.Join(strings.Fields(x), " ")
	default:
		return strings.Join(strings.Fields(fmt.Sprintf("%v", x)), " ")
	}
}

// mongoBuildResultSet monta o ResultSet a partir de uma amostra de
// documentos já achatados: a coluna "_id" sempre vem primeiro quando
// presente, o resto em ordem alfabética — determinístico entre chamadas,
// mesmo com documentos de formato variável. Colunas ausentes num documento
// específico (ex.: veio de um formato mais antigo) viram "∅", igual valor
// nulo em qualquer outra engine.
func mongoBuildResultSet(docs []bson.M, query string, elapsed time.Duration) *ResultSet {
	flat := make([]map[string]string, len(docs))
	seen := map[string]bool{}
	for i, doc := range docs {
		f := map[string]string{}
		mongoFlattenInto(doc, "", mongoFlattenDepth, f)
		flat[i] = f
		for k := range f {
			seen[k] = true
		}
	}

	var cols []string
	if seen["_id"] {
		cols = append(cols, "_id")
		delete(seen, "_id")
	}
	rest := make([]string, 0, len(seen))
	for k := range seen {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	cols = append(cols, rest...)

	rs := &ResultSet{Columns: cols, Query: query, Language: "mongo", Elapsed: elapsed}
	for _, f := range flat {
		row := make([]string, len(cols))
		for i, c := range cols {
			if v, ok := f[c]; ok {
				row[i] = v
			} else {
				row[i] = "∅"
			}
		}
		rs.Rows = append(rs.Rows, row)
	}
	return rs
}

type mongoSession struct {
	client  *mongo.Client
	cont    *mongoContainer
	restore *RestoreResult
	// dbNames são as databases que o restore produziu (sem as internas do
	// servidor) — o que Collections/Health enxergam como o backup.
	dbNames []string
}

// mongoHealthName resume o nome exibido no cabeçalho de Health: a única
// database, quando há só uma (o caso comum de um mongodump --db); a lista
// separada por vírgula quando o archive trouxe várias (dump do servidor
// inteiro).
func mongoHealthName(dbNames []string) string {
	if len(dbNames) == 0 {
		return "(vazio)"
	}
	return strings.Join(dbNames, ", ")
}

func (s *mongoSession) Health(ctx context.Context) (*Health, error) {
	var totalCollections, totalIndexes int64
	var totalSize int64
	for _, dbName := range s.dbNames {
		var stats bson.M
		if err := s.client.Database(dbName).RunCommand(ctx, bson.D{{Key: "dbStats", Value: 1}}).Decode(&stats); err != nil {
			return nil, err
		}
		totalCollections += mongoStatsInt(stats, "collections")
		totalIndexes += mongoStatsInt(stats, "indexes")
		totalSize += mongoStatsInt(stats, "dataSize")
	}
	return &Health{
		Name: mongoHealthName(s.dbNames),
		Size: humanSize(totalSize),
		Fields: []HealthField{
			{"coleções", fmt.Sprint(totalCollections)},
			{"índices", fmt.Sprint(totalIndexes)},
		},
	}, nil
}

// mongoStatsInt lê um campo numérico de um resultado de comando (dbStats,
// collStats), tolerando os tipos numéricos que o driver pode decodificar
// (int32, int64, float64) — o comando devolve tipos diferentes dependendo da
// magnitude do valor.
func mongoStatsInt(stats bson.M, field string) int64 {
	switch v := stats[field].(type) {
	case int32:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}

// Collections varre todas as databases restauradas (exceto as internas do
// servidor) e lista suas coleções, exceto as de sistema (system.*) — uma
// coleção vazia aparece com contagem zero, nunca omitida (mesmo contrato das
// demais engines).
func (s *mongoSession) Collections(ctx context.Context, exact bool) ([]Collection, error) {
	var out []Collection
	for _, dbName := range s.dbNames {
		db := s.client.Database(dbName)
		names, err := db.ListCollectionNames(ctx, bson.D{})
		if err != nil {
			return nil, err
		}
		sort.Strings(names)
		for _, name := range names {
			if strings.HasPrefix(name, "system.") {
				continue
			}
			col := db.Collection(name)

			var count int64
			if exact {
				count, err = col.CountDocuments(ctx, bson.D{})
			} else {
				count, err = col.EstimatedDocumentCount(ctx)
			}
			if err != nil {
				return nil, err
			}

			var stats bson.M
			var sizeBytes int64
			if err := db.RunCommand(ctx, bson.D{{Key: "collStats", Value: name}}).Decode(&stats); err == nil {
				sizeBytes = mongoStatsInt(stats, "size")
			}

			var sample bson.M
			_ = col.FindOne(ctx, bson.D{}).Decode(&sample) // erro (ex.: ErrNoDocuments) só significa "sem amostra"
			d := mongoDescriptor{OrderField: mongoChooseOrderField(sample)}

			out = append(out, Collection{
				Namespace:  dbName,
				Name:       name,
				Count:      count,
				Size:       humanSize(sizeBytes),
				Hint:       mongoOrderHint(d.OrderField),
				Preview:    mongoRecentQuery(dbName, name, d),
				Descriptor: d,
			})
		}
	}
	return out, nil
}

func (s *mongoSession) Recent(ctx context.Context, c Collection) (*ResultSet, error) {
	d, _ := c.Descriptor.(mongoDescriptor)
	field := "_id"
	if d.OrderField != "" {
		field = d.OrderField
	}

	start := time.Now()
	col := s.client.Database(c.Namespace).Collection(c.Name)
	cur, err := col.Find(ctx, bson.D{}, options.Find().SetSort(bson.D{{Key: field, Value: -1}}).SetLimit(mongoRecentLimit))
	if err != nil {
		return nil, err
	}
	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	return mongoBuildResultSet(docs, mongoRecentQuery(c.Namespace, c.Name, d), time.Since(start)), nil
}

// Query roda um comando nativo do Mongo — um documento JSON (ex.:
// {"ping":1}), executado via runCommand contra a database "admin" (o mesmo
// alvo que um `db.runCommand(...)` digitado no mongosh a partir de qualquer
// database usaria para comandos administrativos). Não há UI para digitar
// consulta livre nesta entrega (SPEC.md, "Fora do escopo"); a interface
// existe e é usada internamente e pela suíte de conformidade.
func (s *mongoSession) Query(ctx context.Context, raw string) (*ResultSet, error) {
	var cmd bson.D
	if err := bson.UnmarshalExtJSON([]byte(raw), false, &cmd); err != nil {
		return nil, fmt.Errorf("comando inválido (esperado JSON, ex.: {\"ping\":1}): %w", err)
	}

	start := time.Now()
	var result bson.M
	if err := s.client.Database("admin").RunCommand(ctx, cmd).Decode(&result); err != nil {
		return nil, err
	}
	rs := &ResultSet{Query: raw, Language: "mongo", Columns: []string{"resultado"}, Elapsed: time.Since(start)}
	rs.Rows = append(rs.Rows, []string{mongoCompactJSON(result)})
	return rs, nil
}

func (s *mongoSession) ConnectHint() ConnectHint {
	dsn := s.cont.URI()
	if len(s.dbNames) == 1 {
		dsn += "/" + s.dbNames[0]
	}
	return ConnectHint{
		Name:      s.cont.Name,
		DSN:       dsn,
		Shell:     fmt.Sprintf("mongosh %q", dsn),
		ExecShell: fmt.Sprintf("docker exec -it %s mongosh", s.cont.Name),
		Remove:    fmt.Sprintf("docker rm -f %s", s.cont.Name),
		Port:      s.cont.Port,
	}
}

func (s *mongoSession) Restore() *RestoreResult { return s.restore }

func (s *mongoSession) Close() error {
	if s.client != nil {
		s.client.Disconnect(context.Background())
	}
	s.cont.Remove()
	return nil
}
