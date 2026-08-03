# Suporte multi-engine para verificação de backups

Status: ready-for-agent

## Problem Statement

Hoje o `db-verify` só sabe verificar backups do PostgreSQL em formato `pg_dump`
(`custom`/`plain`/`tar`, opcionalmente com gzip). Quem administra uma stack real quase
nunca tem só Postgres: tem um MySQL/MariaDB de app legado, um SQLite embarcado, um Redis
de cache/fila com `dump.rdb`, um MongoDB. Para cada um desses o operador precisa hoje:

1. Lembrar o comando de restore específico (`mysql <`, `mongorestore`, `redis-server --dir`, …);
2. Subir o container na mão, com a versão certa da imagem;
3. Descobrir sozinho se o restore deu certo — se as tabelas/coleções/chaves existem e têm dados;
4. Abrir um cliente separado para olhar os registros mais recentes;
5. Lembrar de derrubar o container depois.

Isso é exatamente o trabalho que o `db-verify` já automatiza para Postgres, só que a
automação está grudada no Postgres: a detecção de formato assume `PGDMP`, o container
assume `postgres:<v>-alpine`, o restore assume `pg_restore`, a introspecção assume
`information_schema`/`pg_catalog` via `pgx`, e a TUI assume "schema.tabela".

O resultado prático: o operador confia nos backups de Postgres e torce pelo resto. Um
`.rdb` corrompido ou um `mysqldump` truncado só é descoberto no dia da restauração real.

## Solution

O `db-verify` passa a ser um verificador de backups **agnóstico de banco**. O usuário
aponta para um arquivo de backup — de qualquer engine suportada — e o fluxo é o mesmo de
sempre:

```
db-verify data/loja.sql.gz
db-verify data/dump.rdb
db-verify data/app.sqlite
db-verify data/mongo-backup.archive
db-verify --engine mysql backup_sem_extensao
```

O programa detecta a engine pelo conteúdo do arquivo (com fallback por extensão e
override manual por flag), sobe o container correspondente na versão certa, restaura,
e abre a mesma TUI: painel esquerdo com as **coleções** do backup (tabelas no mundo
relacional, coleções no Mongo, grupos de chaves no Redis) e painel direito com os
**20 registros mais recentes** daquela coleção, mais o resumo de saúde no cabeçalho.

O picker de arquivos em `data/` passa a listar todos os formatos reconhecidos, mostrando
qual engine foi detectada para cada um antes de o usuário escolher.

Engines nesta entrega: **PostgreSQL** (paridade com hoje), **MySQL**, **MariaDB**,
**SQLite**, **Redis**, **MongoDB**.

## User Stories

### Detecção e seleção de arquivo

1. Como operador de infraestrutura, quero apontar o `db-verify` para qualquer arquivo de backup suportado, para não precisar decorar o comando de restore de cada banco.
2. Como operador, quero que a engine seja detectada pelo conteúdo do arquivo (magic bytes / cabeçalho), para que backups com nome ou extensão atípicos ainda funcionem.
3. Como operador, quero que a extensão seja usada como desempate quando o conteúdo é ambíguo, para que `.sql`, `.sqlite`, `.rdb`, `.archive` e `.dump` levem à engine certa.
4. Como operador, quero uma flag `--engine` para forçar a engine manualmente, para destravar o caso em que a detecção erra.
5. Como operador, quero uma mensagem de erro clara quando nenhuma engine reconhece o arquivo, listando as engines disponíveis e o que cada uma espera, para eu saber se é arquivo corrompido ou formato não suportado.
6. Como operador, quero que o picker de `data/` liste todos os arquivos de backup reconhecidos — não só `.dump` — para eu escolher entre backups de bancos diferentes na mesma tela.
7. Como operador, quero ver no picker qual engine foi detectada para cada arquivo, para confirmar a detecção antes de gastar tempo subindo container.
8. Como operador, quero que arquivos não reconhecidos apareçam no picker marcados como "desconhecido" em vez de sumirem, para eu perceber que existe um arquivo lá que o programa não entende.
9. Como operador, quero que backups comprimidos com gzip continuem funcionando para todas as engines, para não ter que descomprimir na mão.
10. Como operador, quero que a detecção enxergue *através* do gzip (descomprimir o cabeçalho antes de identificar), para que `dump.sql.gz` e `backup.rdb.gz` sejam detectados corretamente.
11. Como operador, quero suporte a `zstd` e `bzip2` além de gzip, para cobrir os formatos que as ferramentas de backup modernas produzem.
12. Como operador, quero que a detecção não leia o arquivo inteiro, para que apontar para um dump de 40 GB não trave o programa antes de começar.

### Provisionamento e restore

13. Como operador, quero que o container suba com a imagem correta da engine detectada, para não ter que configurar nada.
14. Como operador, quero que a versão da imagem seja inferida do próprio backup quando o formato carrega essa informação, para restaurar num servidor compatível.
15. Como operador, quero um fallback de versão bem documentado por engine quando o backup não informa a versão, para o comportamento ser previsível.
16. Como operador, quero uma flag genérica de versão da imagem que funcione para qualquer engine, para não precisar de uma flag por banco.
17. Como operador, quero que `--pg` continue funcionando como alias da flag genérica, para não quebrar os scripts que já uso.
18. Como operador, quero que o container suba com as otimizações de "dado descartável" de cada engine (fsync desligado, durabilidade relaxada), para o restore ser rápido.
19. Como operador, quero que o backup seja transmitido para dentro do container por stream, para não estourar disco temporário duplicando um dump grande.
20. Como operador, quero ver os erros do restore resumidos na saída, com o log completo em arquivo, independentemente da engine, para diagnosticar um backup problemático.
21. Como operador, quero que erros de restore de cada engine sejam reconhecidos como erros de verdade (não só as strings do `pg_restore`), para não receber um "✓ sem erros" falso.
22. Como operador, quero que um restore que falha completamente seja distinguido de um restore com erros parciais, para saber se o backup é lixo ou só tem problemas de permissão/owner.
23. Como operador, quero que a porta publicada no host seja escolhida automaticamente e seja livre, respeitando a porta padrão de cada engine como ponto de partida.
24. Como operador, quero que `--port` continue funcionando para fixar a porta manualmente, para plugar meu cliente gráfico.
25. Como operador, quero que `--keep` funcione em todas as engines e imprima o comando de shell certo para aquele banco (`psql`, `mysql`, `redis-cli`, `mongosh`), para continuar investigando depois que a TUI fecha.
26. Como operador, quero que o container seja sempre removido ao sair, inclusive em `ctrl+c` e em erro no meio do restore, para não deixar lixo rodando na minha máquina.
27. Como operador de SQLite, quero que o arquivo seja verificado **sem container** quando não houver necessidade de servidor, para o fluxo ser instantâneo.
28. Como operador, quero uma barra ou indicação de progresso durante restores longos, para saber que o programa não travou.
29. Como operador, quero que o timeout de "ficar pronto" seja específico da engine, para que um Mongo que demora mais a subir não seja abortado cedo demais.

### Introspecção e saúde

30. Como operador, quero ver um resumo de saúde no cabeçalho para qualquer engine, para bater o olho e saber se o backup faz sentido.
31. Como operador relacional, quero ver contagem de tabelas, views, índices, funções e chaves estrangeiras, como hoje.
32. Como operador de MySQL/MariaDB, quero ver também procedures/triggers, para saber se a lógica no banco veio junto.
33. Como operador de MongoDB, quero ver contagem de coleções, índices e tamanho total, para o equivalente ao resumo relacional.
34. Como operador de Redis, quero ver total de chaves, quebra por tipo (string/hash/list/set/zset/stream), memória usada e quantas chaves têm TTL, para avaliar o snapshot.
35. Como operador, quero que campos de saúde que não se aplicam à engine sejam omitidos em vez de mostrados zerados, para o cabeçalho não mentir.
36. Como operador, quero ver a listagem de coleções com nome, quantidade de registros e tamanho, igual hoje, em qualquer engine.
37. Como operador, quero poder trocar entre contagem exata e estimada, porque contagem exata em banco grande é cara — como hoje com `--no-counts`.
38. Como operador de Redis, quero que as "coleções" sejam grupos de chaves inferidos por prefixo (`user:*`, `session:*`), para navegar um keyspace grande sem listar milhões de chaves.
39. Como operador, quero que o filtro `/` funcione sobre a listagem de coleções em qualquer engine, para achar rápido o que procuro.
40. Como operador, quero que coleções vazias apareçam esmaecidas, como hoje, para identificar de relance o que não veio no backup.
41. Como operador, quero um erro claro quando o backup restaura mas não produz nenhuma coleção, para saber que o arquivo está vazio ou corrompido.

### Registros recentes

42. Como operador, quero ver os 20 registros mais recentes de cada coleção, em qualquer engine, porque é o teste rápido de "esse backup tem dados de verdade".
43. Como operador relacional, quero que a coluna de ordenação seja escolhida pela mesma heurística de hoje (nomes de criação → publicação → atualização → qualquer data → PK simples).
44. Como operador de MySQL/MariaDB, quero que a heurística de coluna funcione igual, incluindo os nomes em português já suportados.
45. Como operador de MongoDB, quero ordenação por `_id` decrescente quando não houver campo de data óbvio, porque o ObjectId carrega o timestamp de criação.
46. Como operador de MongoDB, quero ver documentos aninhados achatados em colunas legíveis, para o painel continuar sendo uma tabela.
47. Como operador de Redis, quero ver uma amostra de chaves do grupo com tipo, TTL e um preview do valor, já que "mais recente" não existe no Redis.
48. Como operador, quero ver na tela a consulta exata que foi executada, para poder copiar e rodar no meu cliente.
49. Como operador, quero que a consulta exibida esteja na linguagem nativa da engine (SQL, comando Mongo, comando Redis), para ela ser realmente copiável.
50. Como operador, quero ver quanto tempo a consulta levou e quantas linhas voltaram, como hoje.
51. Como operador, quero rolar as colunas horizontalmente em qualquer engine, para ver resultados largos.
52. Como operador, quero recarregar a consulta com `enter`/`r`, como hoje.
53. Como operador, quero que valores nulos/ausentes/binários sejam renderizados de forma consistente entre engines, para o painel ser legível.

### Rede de segurança antes da refatoração

54. Como mantenedor, quero uma bateria de testes de funcionalidade do comportamento atual **antes** de qualquer refatoração, para ter confiança de que a extração da interface não quebrou nada.
55. Como mantenedor, quero que esses testes descrevam o que o programa faz hoje com Postgres (detecção de formato, restore, listagem de tabelas, escolha da coluna de ordenação, 20 mais recentes), para servirem de baseline.
56. Como mantenedor, quero que esses testes continuem passando sem alteração depois da refatoração, porque um teste que precisa ser reescrito não prova nada.
57. Como mantenedor, quero que essa fase seja só de teste de funcionalidade — sem lint obrigatório, sem cobertura mínima, sem CI bloqueante — para não travar a entrega em burocracia agora.

### Extensibilidade e operação

58. Como mantenedor, quero adicionar uma engine nova implementando uma única interface e registrando-a, sem tocar em `main.go` nem na TUI, para o custo marginal de cada banco ser baixo.
59. Como mantenedor, quero uma suíte de conformidade que qualquer engine nova precise passar, para garantir que ela se comporta como as outras antes de entrar.
60. Como mantenedor, quero que a TUI não tenha nenhum `if engine == …`, para a interface não virar um emaranhado de casos especiais.
61. Como operador, quero listar as engines suportadas por linha de comando, para saber o que a versão instalada aceita.
62. Como operador, quero que uma engine faltando a imagem Docker seja reportada como erro claro e acionável, para eu saber que preciso de rede ou de um `docker pull`.
63. Como operador em ambiente sem Docker, quero que apenas as engines que exigem container falhem, e que SQLite continue funcionando, para não ficar sem ferramenta nenhuma.
64. Como operador, quero que o README documente cada engine suportada, o formato de backup esperado e o comando que gera esse backup, para eu conseguir produzir um arquivo verificável.

## Implementation Decisions

### Seam único: `Engine` + `Session`

Toda a variação por banco fica atrás de **uma** interface, decidida com o usuário. Nada
mais no programa conhece Postgres, Redis ou Mongo.

```go
// Registro global; cada engine se registra no init do seu arquivo.
func Register(e Engine)
func Engines() []Engine
func Lookup(name string) (Engine, bool)

type Engine interface {
    Name() string                                    // "postgres", "mysql", "redis"…
    // Detect recebe o cabeçalho JÁ descomprimido e o caminho original.
    // Devolve confiança para o registro desempatar entre engines.
    Detect(head []byte, path string) (Match, bool)
    Provision(ctx context.Context, b *Backup, opts ProvisionOpts) (Session, error)
}

type Match struct {
    Format     string // "custom", "plain", "rdb", "archive", "sqlite3"…
    Version    string // versão da engine de origem, "" se desconhecida
    OriginDB   string
    Confidence int    // 100 = magic bytes; 50 = extensão; 10 = palpite
}

type Session interface {
    Health(ctx context.Context) (*Health, error)
    Collections(ctx context.Context, exact bool) ([]Collection, error)
    Recent(ctx context.Context, c Collection) (*ResultSet, error)
    Query(ctx context.Context, raw string) (*ResultSet, error)
    ConnectHint() ConnectHint // DSN + comando de shell para --keep
    Close() error
}
```

`Provision` é deliberadamente grosso: engloba subir o container, esperar ficar pronto,
copiar o backup, restaurar e conectar. Isso mantém o número de seams em um. O
`RestoreResult` (erros, duração, log, exit code) passa a ser carregado pela `Session`
e lido pela TUI, em vez de ser devolvido separadamente por `main.go`.

### Modelo de dados genérico

- `Backup` substitui `DumpInfo`: caminho, tamanho, compressão, engine detectada, formato,
  versão de origem, banco de origem. Ganha o campo `Engine` e perde `PGMajor` (vira `Version`).
- `Collection` substitui `TableInfo`: `Namespace` (schema/database/prefixo), `Name`,
  `Count`, `Size`, e um `Descriptor` opaco por engine que a `Session` usa para montar a
  consulta recente. A TUI só lê `Namespace`/`Name`/`Count`/`Size` e passa a `Collection`
  de volta para `Recent` — nunca monta SQL.
- `ResultSet` ganha o campo `Language` (`"sql"`, `"mongo"`, `"redis"`) e `Query` passa a
  ser o texto nativo da consulta, para a TUI exibir sem interpretar.
- `Health` passa de struct de campos fixos para um resumo ordenado de pares
  rótulo/valor mais alguns campos universais (`Name`, `Size`), para que cada engine
  publique só o que faz sentido. A TUI renderiza a lista que receber.

A escolha de coluna de ordenação (heurística de `created_at`/`data_criacao`/…) sai do SQL
inline e vira uma lista de nomes compartilhada, consumida pelas engines relacionais —
Postgres e MySQL/MariaDB usam a mesma lista, cada uma no seu dialeto de
`information_schema`. Isso preserva o comportamento atual sem duplicar a heurística.

### Detecção

A detecção vira um passo em duas fases, dentro do registro (não é um seam separado):

1. Abrir o arquivo, descomprimir o cabeçalho se for gzip/zstd/bzip2, ler ~8 KB.
2. Perguntar a cada engine registrada; a de maior `Confidence` vence; empate resolve por
   ordem de registro. `--engine` pula a fase inteira.

Assinaturas por engine: `PGDMP` e `PostgreSQL database dump` (Postgres); cabeçalho
`-- MySQL dump` / `/*!40101` e variantes MariaDB; `SQLite format 3\0` (SQLite);
`REDIS` seguido da versão do RDB (Redis); cabeçalho de `mongodump --archive` (Mongo).
Extensões (`.sql`, `.sqlite`/`.db`, `.rdb`, `.archive`, `.bson`, `.dump`) entram como
sinal de confiança média.

Ambiguidade conhecida e resolvida por decisão: um `.sql` plain sem cabeçalho identificável
é atribuído ao Postgres (comportamento de hoje), com aviso na saída e sugestão de `--engine`.

### Por engine

| Engine | Imagem | Restore | Introspecção | Coleções |
|---|---|---|---|---|
| PostgreSQL | `postgres:<v>-alpine` | `pg_restore` / `psql` | `pg_catalog` via `pgx` | schema.tabela |
| MySQL | `mysql:<v>` | `mysql <` | `information_schema` | database.tabela |
| MariaDB | `mariadb:<v>` | `mariadb <` | `information_schema` | database.tabela |
| SQLite | nenhuma (in-process) | cópia do arquivo | `sqlite_master`, `PRAGMA` | tabela |
| Redis | `redis:<v>-alpine` | `.rdb` no datadir antes do start | `INFO`, `DBSIZE`, `SCAN` | prefixo de chave |
| MongoDB | `mongo:<v>` | `mongorestore --archive` | `listCollections`, `dbStats` | db.coleção |

Decisões específicas:

- **MySQL/MariaDB** são engines distintas no registro (imagens e cabeçalhos diferentes),
  mas compartilham a maior parte da implementação relacional.
- **SQLite** não usa Docker: o arquivo é copiado para um temporário e aberto direto. A
  ausência de Docker não impede essa engine. O "restore" é a cópia mais um
  `PRAGMA integrity_check`, cujo resultado alimenta os erros do `RestoreResult`.
- **Redis** não tem comando de restore: o `.rdb` é colocado no diretório de dados do
  container **antes** de o servidor subir. Se o RDB for inválido, o container não fica
  pronto — esse caso vira erro de restore com o log do container, não timeout genérico.
- **Redis** agrupa chaves por prefixo até o primeiro separador (`:` por padrão), via
  `SCAN` amostrado com limite configurável. "20 mais recentes" vira "20 chaves do grupo"
  com tipo, TTL e preview do valor; a TUI mostra isso como qualquer outro resultset.
- **MongoDB** usa `mongorestore --archive` lendo do stdin, evitando arquivo temporário
  dentro do container. Documentos são achatados com caminho pontilhado (`endereco.cidade`)
  até uma profundidade fixa; arrays viram JSON compacto.
- Contagem exata vs. estimada mapeia para o equivalente de cada engine (`count(*)` vs.
  `n_live_tup`; `COUNT(*)` vs. `information_schema.TABLES.TABLE_ROWS`;
  `countDocuments` vs. `estimatedDocumentCount`; `DBSIZE` sempre exato no Redis).

### CLI

- Nova flag `--engine <nome>` para forçar a engine; `--list-engines` para enumerar.
- Nova flag `--version-tag` (genérica) para a versão da imagem; `--pg` vira alias
  deprecado com aviso, preservando os scripts existentes.
- `--jobs` passa a ser interpretado por engine (paralelismo do `pg_restore`, de
  `mongorestore`; ignorado onde não se aplica, com aviso apenas se explicitamente passado).
- `--port`, `--db`, `--keep`, `--no-counts` mantêm significado atual, agora genéricos.
- O picker mostra uma coluna nova com a engine detectada.

### Não muda

O contrato visível da TUI: mesmo layout de três painéis, mesmas teclas, mesmo cabeçalho,
mesma remoção garantida do container ao sair. Um usuário que só verifica Postgres não deve
notar diferença além do texto de ajuda.

## Testing Decisions

O repositório **não tem testes hoje**. Esta spec introduz a primeira suíte, e o critério
é: testar comportamento externo observável, nunca a forma interna. Um bom teste aqui
descreve o que o operador vê ("um `.rdb` válido produz grupos de chaves com contagem
correta"), não como o código chega lá.

**Camada 0 — caracterização do comportamento atual, escrita ANTES de qualquer refatoração.**
Esta é a primeira coisa a ser feita, com nenhuma linha de produção alterada até ela estar
verde. O objetivo é confiança: travar o comportamento que o Postgres tem hoje para que a
extração do `Engine`/`Session` seja provadamente sem regressão. Ela cobre, contra o código
como está:

- detecção de formato e versão a partir de cabeçalhos reais (`PGDMP` custom, plain SQL,
  tar, cada um também gzipado), incluindo o banco de origem extraído do cabeçalho;
- fluxo completo de restore contra um dump Postgres pequeno gerado no próprio teste:
  restore sem erros, listagem de tabelas com contagem exata correta, tabela vazia
  aparecendo com zero;
- a heurística de coluna de ordenação — para cada família de nome suportada hoje
  (`created_at`, `data_criacao`, `published_at`, `updated_at`, timestamp genérico, PK
  simples, e o caso sem nenhuma opção), asserta a coluna escolhida e o SQL resultante;
- os 20 mais recentes: no máximo 20 linhas, ordem decrescente pela coluna escolhida;
- formatação de valores (nulo, data, data-hora, binário, string com quebra de linha);
- remoção do container ao encerrar, e preservação com `--keep`.

Regra que dá o valor todo a esta camada: **esses testes não podem ser reescritos durante a
refatoração**. Se um deles precisar mudar, ou o comportamento regrediu de verdade, ou o
teste estava amarrado à forma interna e deveria ter sido escrito de outro jeito. Onde a
assinatura mudar por força da refatoração (por exemplo `FetchTables` virando
`Session.Collections`), a adaptação permitida é só a chamada — as asserções ficam intactas.

Esta fase é **só teste de funcionalidade**. Sem quality gate: nenhum lint obrigatório,
nenhuma meta de cobertura, nenhum CI bloqueante, nenhum hook de pre-commit. Essas coisas
podem vir depois, em spec própria; entrar com elas agora só atrasaria a entrega.

**Camada 1 — detecção (unidade, rápida, sem Docker).** Fixtures pequenas de cabeçalho
(algumas centenas de bytes reais de cada formato, versionadas no repo em
`testdata/headers/`) alimentadas no registro; asserta engine, formato e versão detectados.
Cobre também: as mesmas fixtures gzipadas/zstdadas, arquivo vazio, arquivo de texto
aleatório (deve falhar com erro nomeando as engines), e o caso ambíguo do `.sql` sem
cabeçalho (deve cair em Postgres com aviso). Esta camada roda em qualquer máquina e
guarda a maior parte da lógica arriscada.

**Camada 2 — suíte de conformidade da `Session` (integração, exige Docker).** Um único
corpo de teste parametrizado por engine, atrás de uma build tag (`//go:build docker`),
que para cada engine: gera um backup mínimo conhecido (2 tabelas/coleções — uma com
linhas e uma vazia, uma com coluna de data e uma sem), roda `Provision`, e asserta o
contrato:

- restore reporta zero erros para o backup válido;
- `Health` devolve nome e tamanho não vazios e nenhum contador negativo;
- `Collections` devolve exatamente as coleções esperadas, com contagem exata correta;
- a coleção vazia aparece com contagem zero (não é omitida);
- `Recent` devolve no máximo 20 linhas, e para a coleção com coluna de data devolve em
  ordem decrescente;
- `Query` com um comando nativo válido devolve resultado; com um inválido devolve erro,
  não pânico;
- `Close` remove o container — verificado por `docker ps` depois.

Rodar o mesmo corpo contra todas as engines é o que garante que "adicionar uma engine"
tem um critério objetivo de pronto. Um teste-negativo adicional por engine usa um backup
deliberadamente truncado e asserta que erros de restore são reportados e não engolidos.

**Camada 3 — regressão do Postgres.** Os dumps reais em `data/` não vão para o repo
(estão no `.gitignore`, podem conter dados sensíveis). Em vez disso, um teste opcional
lê um caminho de dump por variável de ambiente e roda o fluxo completo, para o mantenedor
validar contra um backup de produção antes de soltar uma release.

**Fora da suíte:** a renderização da TUI não é testada por snapshot — o valor é baixo e a
manutenção alta. O que a TUI consome (`Health`, `Collection`, `ResultSet`) é testado nas
camadas acima; a TUI só formata.

**Prior art:** não existe no repositório. As convenções desta spec passam a ser o
precedente: `testing` da biblioteca padrão, sem framework de asserção, table-driven,
fixtures em `testdata/`, integração atrás de build tag.

## Out of Scope

- **Comparar dois backups entre si** ou detectar drift entre backup e produção.
- **Verificar integridade criptográfica** (checksums, assinaturas GPG) do arquivo.
- **Backups criptografados** — se o arquivo estiver cifrado, a detecção falha com erro claro.
- **Backups multi-arquivo / diretório** (`pg_dump -Fd`, `mongodump` sem `--archive`,
  `xtrabackup`) — apenas arquivo único nesta entrega.
- **Restaurar de storage remoto** (S3, GCS). O `s3check`, projeto irmão no mesmo
  diretório pai, é o lugar natural para isso; a integração entre os dois fica para depois.
- **Escrever no banco restaurado** pela TUI — a ferramenta segue somente-leitura.
- **Editor de consulta livre na TUI** — a `Session.Query` existe na interface e é usada
  internamente, mas não há UI para digitar consulta arbitrária nesta entrega.
- **SQL Server, Oracle, ClickHouse, Cassandra, Elasticsearch, InfluxDB** — o registro
  torna cada um deles um acréscimo barato depois, mas nenhum entra agora.
- **Instalar Docker ou puxar imagens proativamente** — a ferramenta reporta a falta, não a resolve.
- **Internacionalização** — a interface continua em português.

## Further Notes

- O README já antecipa esta mudança ("o nome já prevê suporte a outros bancos no futuro"),
  então o nome do binário e do módulo não mudam.
- A ordem de implementação sugerida extrai o valor mais cedo e valida o seam com o caso
  mais diferente logo no começo: **(0)** escrever a camada 0 de caracterização contra o
  código atual, sem alterar produção, e só seguir com ela verde; **(1)** extrair a
  interface e reimplementar Postgres atrás dela, sem mudança visível de comportamento —
  a camada 0 continuando verde é o critério de pronto desta etapa; **(2)** MySQL/MariaDB, que exercitam o
  caminho relacional alternativo; **(3)** Redis, que é o caso que mais tensiona o modelo
  genérico — se `Collection`/`ResultSet` sobrevivem ao Redis, sobrevivem a qualquer coisa;
  **(4)** SQLite, que valida o caminho sem container; **(5)** MongoDB.
- Fazer o Redis antes do SQLite é intencional: SQLite é fácil e não descobre problemas de
  design; Redis descobre. Descobrir cedo é mais barato.
- O risco principal é o `Health` genérico virar o mínimo denominador comum e perder
  informação útil do Postgres. A mitigação é o resumo em pares rótulo/valor, que deixa
  cada engine publicar o que tem de específico sem inventar campos vazios nas outras.
- O segundo risco é a suíte de integração ficar lenta demais para rodar no dia a dia.
  Por isso ela está atrás de build tag, e a camada de detecção — que concentra a lógica
  que mais quebra — roda sem Docker.
