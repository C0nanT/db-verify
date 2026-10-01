# Contrato de `engine` alinhado ao código (achado 7 do mapa de dívida do core)

Status: ready-for-agent

Origem: `.scratch/tech-debt-map/core/2026-09-29.md`, achado 7 (Manutenibilidade, 🟢 Low, Contained). Parte já resolvida: a referência ao identificador inexistente `guessConfidence` saiu.

## Problem Statement

Quem lê o contrato em `internal/engine` (para escrever uma engine nova, ajustar uma existente ou entender o `main`) encontra duas informações que não batem com o código:

1. `ProvisionOpts` tem o campo `ExactCounts`. O `main` preenche esse campo a partir da flag de contagem exata, e os testes também, mas nenhuma engine o lê. Quem controla de fato se a contagem é exata é o parâmetro `exact` de `Session.Collections`. Por isso, quem procurar mudar o comportamento de contagem pelo `Provision` muda um campo morto, não vê efeito nenhum, e nada avisa.
2. O campo `Match.Confidence` repete num comentário os valores `100/50/10`, ao lado das constantes `ConfidenceMagic`, `ConfidenceExtension` e `ConfidenceGuess`, que já são a fonte desses números. Se uma constante mudar, o comentário vai continuar dizendo o valor antigo.

## Solution

O contrato passa a descrever só o que existe:

- `ExactCounts` sai de `ProvisionOpts`. A contagem exata continua sendo pedida por `Session.Collections(ctx, exact)`, o único canal que já funciona. A flag de linha de comando e o comportamento do usuário não mudam.
- O comentário de `Match.Confidence` deixa de repetir os números e passa a apontar para as constantes `Confidence*`.

Nenhum comportamento observável muda. É limpeza de contrato.

## User Stories

1. Como autor de uma engine nova, quero que `ProvisionOpts` liste só parâmetros que alguma engine pode usar, para não implementar suporte a um campo que nenhum chamador espera que faça efeito.
2. Como autor de uma engine nova, quero que o contrato deixe claro que a contagem exata é pedida em `Session.Collections`, para implementar o modo exato no lugar certo.
3. Como mantenedor do `main`, quero que a flag de contagem exata chegue às engines por um único caminho, para não ter que manter dois canais em sincronia.
4. Como mantenedor, quero que remover um campo morto quebre a compilação de quem ainda o preenche, para nenhum ponto de atribuição esquecido sobreviver em silêncio.
5. Como leitor do contrato, quero que o comentário de `Match.Confidence` aponte para as constantes nomeadas em vez de repetir os valores, para o comentário não envelhecer quando uma constante mudar.
6. Como usuário do CLI, quero que a flag de contagem exata continue funcionando igual, para a limpeza interna não mudar o que vejo na tela.
7. Como mantenedor dos testes de conformidade e do fluxo do Postgres, quero que as opções de provisionamento dos testes não carreguem um campo sem efeito, para os testes não sugerirem que o `Provision` depende dele.

## Implementation Decisions

- **Módulo afetado: `internal/engine` (policy).** Remover o campo `ExactCounts` de `ProvisionOpts`. A assinatura de `Session.Collections(ctx, exact bool)` fica como está: ela é o contrato real.
- **Chamadores de `ProvisionOpts` que preenchem o campo:** o `main` (montagem das opções em `run()`), a suíte de conformidade genérica na raiz (opções padrão da suíte) e o teste de fluxo completo do Postgres em `engines/postgres` (tag `docker`). Os três param de preencher o campo. O `main` continua repassando a flag para `Collections`, como já faz.
- **Assinatura de `run()` no `main`:** o parâmetro `exactCounts` continua, porque segue em uso na chamada a `Collections`.
- **Comentário de `Match.Confidence`:** trocar `100 = magic bytes; 50 = extensão; 10 = palpite` por uma referência às constantes `Confidence*` declaradas logo abaixo, sem repetir valores.
- **Sem mudança de comportamento:** nenhuma engine lia o campo, então removê-lo não altera nenhum `Provision`. O compilador é a verificação: qualquer leitor ou atribuição restante vira erro de build.
- **Boundaries:** nenhum import novo. As regras do `depguard` continuam satisfeitas.

## Testing Decisions

- **Seam:** o próprio contrato `Engine`/`Session` e o build. Por ser a remoção de um campo sem leitores, o teste que conta é a compilação de todos os pacotes, inclusive os arquivos com tag `docker`. Não cabe teste unitário novo: ele testaria a ausência de um campo, ou seja, a implementação, não o comportamento.
- **Verificação obrigatória:**
  - `./scripts/check fast` (gofmt, vet, golangci-lint, testes unitários);
  - `go vet -tags docker ./...`, para compilar a conformidade e o `docker_test.go` do Postgres sem precisar subir containers.
- **Opcional:** `./scripts/check full`, para confirmar em runtime que a suíte de conformidade continua verde. A contagem exata já é exercitada pela suíte via `Collections(ctx, true)`.
- **Prior art:** o achado 4 foi verificado do mesmo jeito (movimentação pura, `go vet -tags docker ./...` compilando e `check fast`/`check full` passando).

## Out of Scope

- Outros achados do mapa do core: 5 (guard de `Register` e `TestNoDetectTies`), 8 (`RecentLimit`), 9 (janela de porta do Postgres) e 10 (`DockerHost` como caminho único).
- Renomear ou mudar a flag de contagem exata do CLI, ou a assinatura de `Session.Collections`.
- Mudar os valores das constantes de confiança ou a regra de desempate da detecção.
- As referências órfãs a `SPEC.md` no código (questão aberta do mapa).

## Further Notes

- Ao concluir, registrar no "Andamento" do mapa (`.scratch/tech-debt-map/core/2026-09-29.md`) que o achado 7 foi resolvido. Os abertos passam a ser: 5 (parcial), 8, 9 e 10.
- Os comentários e doc-strings seguem em pt-BR, conforme o `CLAUDE.md`.
