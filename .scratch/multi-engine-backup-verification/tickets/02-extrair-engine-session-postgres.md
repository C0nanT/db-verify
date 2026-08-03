# 02 — Extrair `Engine`/`Session` e reimplementar Postgres atrás da interface

**What to build:** toda a variação por banco passa a ficar atrás de uma única interface,
com um registry global. Postgres vira a primeira implementação, e o comportamento
observável não muda em nada — quem usa a ferramenta para verificar um `.dump` não deve
notar diferença alguma.

`main.go` e a TUI param de conhecer Postgres: nenhum `pgx`, nenhum `information_schema`,
nenhum "schema.tabela" fora da engine. A TUI recebe coleções e resultsets genéricos e só
formata.

Formato do seam, decidido na spec:

```go
type Engine interface {
    Name() string
    Detect(head []byte, path string) (Match, bool)
    Provision(ctx context.Context, b *Backup, opts ProvisionOpts) (Session, error)
}

type Session interface {
    Health(ctx context.Context) (*Health, error)
    Collections(ctx context.Context, exact bool) ([]Collection, error)
    Recent(ctx context.Context, c Collection) (*ResultSet, error)
    Query(ctx context.Context, raw string) (*ResultSet, error)
    ConnectHint() ConnectHint
    Close() error
}
```

`Provision` é deliberadamente grosso — sobe o container, espera ficar pronto, copia o
backup, restaura e conecta — para o número de seams no projeto continuar sendo um.

Os modelos de dados generalizam junto: `DumpInfo` vira `Backup` (com engine e versão no
lugar de `PGMajor`); `TableInfo` vira `Collection` (namespace, nome, contagem, tamanho, e
um descriptor opaco que só a engine interpreta); `Health` vira um resumo ordenado de pares
rótulo/valor mais nome e tamanho, para cada engine publicar só o que faz sentido;
`ResultSet` ganha a linguagem da consulta. A heurística de coluna de ordenação sai de
dentro do SQL e vira uma lista de nomes compartilhada, pronta para as engines relacionais
seguintes usarem no seu próprio dialeto.

**Blocked by:** 01 — Testes de caracterização do comportamento Postgres atual.

**Status:** ready-for-agent

- [ ] Existe um registry onde engines se registram e podem ser buscadas por nome
- [ ] Postgres é a única engine registrada, implementando `Engine` e `Session`
- [ ] `main.go` orquestra o fluxo falando apenas com o registry e as interfaces
- [ ] A TUI não referencia Postgres, `pgx`, SQL ou "schema.tabela" em lugar nenhum
- [ ] A TUI monta a consulta de recentes chamando `Session.Recent` com a `Collection`,
      nunca construindo SQL
- [ ] O resultado do restore (erros, duração, log, exit code) é carregado pela `Session`
      e lido pela TUI
- [ ] O cabeçalho de saúde é renderizado a partir da lista de pares que a engine publica
- [ ] Verificar um `.dump` Postgres produz exatamente a mesma tela e a mesma saída de
      terminal de antes deste ticket
- [ ] Toda a suíte do ticket 01 continua verde, com as asserções intactas — só as
      chamadas se adaptam às novas assinaturas
