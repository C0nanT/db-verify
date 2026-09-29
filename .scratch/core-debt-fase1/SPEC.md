# Core: limpeza garantida no Provision, diagnóstico do Docker e heurística de ordenação num lugar só

Status: ready-for-agent

Origem: linhas 1–4 do mapa de dívida `.scratch/tech-debt-map/core/2026-09-29.md`,
com as decisões registradas lá (desempate por posição ordinal; `DockerHost`
como caminho único para a CLI Docker, cuja migração **não** faz parte deste spec).

## Problem Statement

O operador roda o db-verify contra um backup de produção. Se ele aperta
Ctrl+C durante um restore demorado, ou se o `Provision` falha num passo que
não é conflito de porta (pull da imagem, erro do daemon, cancelamento), o
container `db-verify-<pid>` pode continuar rodando com os dados restaurados.
Ninguém avisa, e o container só aparece depois num `docker ps`. Cada engine
trata a limpeza de um jeito: a do Redis limpa dentro do closure de retry, as
outras não.

Quando o Docker não responde, o operador recebe "docker daemon não está
acessível" sem o motivo real, que pode ser permissão no socket, daemon
parado ou `DOCKER_HOST` errado. Se o daemon está travado, o db-verify fica
parado para sempre sem feedback.

Quem mantém o projeto lê em `relational.go` que a heurística de "coluna de
ordenação" vem "de um lugar só", mas a decisão de fato mora em `mysql.go`.
O SQLite a herda sem aviso, e o Postgres reimplementa em SQL com uma
diferença: quando uma tabela tem duas colunas na mesma camada (por exemplo
`created_at` e `inserted_at`), a coluna escolhida pode mudar entre execuções.
A TUI mostra ao operador um "mais recentes" que não é reproduzível.

A suíte de conformidade genérica depende de helpers definidos no teste de
fluxo completo do Postgres. Mexer nesse teste quebra a compilação da suíte
inteira.

## Solution

- Um `Provision` que falha ou é cancelado sempre devolve erro e nunca deixa
  container para trás, em todas as engines, por contrato. Ctrl+C durante o
  provisionamento cancela o trabalho em andamento e limpa, em vez de matar o
  processo no meio.
- A checagem do Docker tem prazo e, quando falha, mostra a saída real do
  `docker info`.
- A heurística de coluna de ordenação (camadas de nome, camada de "qualquer
  data", fallback para PK e desempate) mora inteira no módulo relacional
  compartilhado. As engines só traduzem o dialeto. No Postgres, o empate
  dentro de uma camada passa a ser decidido pela menor posição ordinal, igual
  a MySQL, MariaDB e SQLite.
- Os helpers da suíte de conformidade passam a morar com a suíte, e não num
  teste específico de engine.

## User Stories

1. Como operador, quero que um Ctrl+C durante o restore derrube o container que o db-verify subiu, para não deixar uma cópia de dados de produção rodando na minha máquina.
2. Como operador, quero que o Ctrl+C durante o provisionamento encerre em poucos segundos com uma mensagem clara, para saber que a interrupção foi tratada e não travou.
3. Como operador, quero que uma falha de pull da imagem não deixe container criado pela metade, para não precisar rodar `docker rm` à mão.
4. Como operador, quero que qualquer erro durante o `Provision` (espera de readiness, cópia, restore, conexão) remova o container, para ter o mesmo comportamento em todas as engines.
5. Como operador usando `--keep`, quero que o container só sobreviva quando o `Provision` terminou com sucesso, para que `--keep` não preserve containers quebrados.
6. Como operador, quero que a mensagem de "docker indisponível" inclua o que o `docker info` respondeu, para distinguir permissão no socket de daemon parado.
7. Como operador, quero que a checagem do Docker desista depois de um prazo fixo quando o daemon está travado, para não ficar olhando um terminal parado.
8. Como operador, quero que a checagem do Docker respeite o cancelamento (Ctrl+C), para conseguir sair a qualquer momento.
9. Como operador, quero que "os mais recentes" de uma tabela com duas colunas de data na mesma camada sejam sempre ordenados pela mesma coluna, para que duas execuções sobre o mesmo backup mostrem o mesmo resultado.
10. Como operador, quero que Postgres, MySQL, MariaDB e SQLite escolham a mesma coluna de ordenação para a mesma estrutura de tabela, para que o comportamento não dependa da engine.
11. Como operador, quero que o hint da TUI ("ordenado por X (data)") continue igual para tabelas sem empate, para que esta mudança não altere o que já funciona.
12. Como mantenedor, quero que a heurística de ordenação (camadas, camada de data, fallback para PK, desempate) fique num único módulo compartilhado, para mudar uma regra editando um lugar só.
13. Como mantenedor, quero que o comentário do módulo relacional liste todos os consumidores reais (Postgres, MySQL/MariaDB, SQLite, e Mongo parcialmente), para saber o raio de impacto de uma mudança na lista de camadas.
14. Como mantenedor, quero que o SQLite não dependa de tipos do MySQL para escolher a coluna de ordenação, para poder mexer no MySQL sem quebrar o SQLite.
15. Como mantenedor, quero que o conjunto de "tipos de data" seja fornecido por cada dialeto à heurística compartilhada, para que cada engine declare os próprios tipos sem copiar a decisão.
16. Como mantenedor, quero que o contrato de `Provision` diga explicitamente que erro ou cancelamento não deixam container para trás, para que uma engine nova saiba o que precisa garantir.
17. Como mantenedor, quero que a suíte de conformidade prove essa garantia para toda engine registrada, cancelando o `Provision` em cada passo, para que nenhuma engine escape com uma exceção implícita.
18. Como mantenedor, quero que o helper de retry de porta remova o container nomeado em qualquer falha, e não só em conflito de porta, para que nenhum caminho de erro precise lembrar disso sozinho.
19. Como mantenedor, quero que a limpeza aconteça mesmo quando o ctx já foi cancelado, para que o próprio cancelamento não impeça o `rm -f`.
20. Como mantenedor, quero que os helpers da suíte de conformidade (checar Docker, checar se o container existe, nome único, buscar coleção por nome) morem com a suíte, para poder aposentar ou reescrever o teste de fluxo completo do Postgres sem quebrar a compilação.
21. Como mantenedor, quero testes unitários sem Docker para a limpeza no retry e para o diagnóstico do `docker info`, para cobrir esses caminhos em `scripts/check fast`.
22. Como mantenedor, quero um teste unitário que trave o desempate por posição ordinal na heurística compartilhada, para que uma refatoração futura não reintroduza a escolha arbitrária.
23. Como mantenedor, quero um caso no teste de camadas do Postgres com duas colunas na mesma camada, para provar que o SQL do Postgres segue a mesma regra que a heurística em Go.
24. Como agente implementando isto, quero que cada passo preserve o comportamento atual, exceto o desempate do Postgres e a limpeza nova, para que os testes existentes continuem verdes a cada commit.

## Implementation Decisions

**Contrato (`engine.go`)**
- O doc de `Engine.Provision` passa a afirmar: se `Provision` devolve erro, inclusive por cancelamento do ctx, não resta container (nem outro recurso externo que ele tenha criado). Um ctx cancelado durante o `Provision` resulta em erro, nunca numa `Session` parcialmente pronta.
- Não há mudança de assinatura em `Engine` nem em `Session`.

**Helper de retry de porta (`DockerHost`)**
- `StartWithPortRetry` executa `rm -f <nome>` em **todo** caminho de falha de `attempt` (conflito, outro erro, ctx cancelado), e não só no conflito. O comando é idempotente, então funciona mesmo quando o container não chegou a existir.
- A remoção usa um contexto desacoplado do cancelamento do chamador (sem cancelamento herdado, com prazo curto próprio). Sem isso, um ctx já cancelado impede o `docker rm` de rodar.
- O comportamento de conflito (aviso, próxima porta, prompt ao esgotar, segundo ciclo) não muda.

**Limpeza nas engines Docker**
- Postgres, MySQL, MariaDB e Mongo: se `startWithPortRetry` devolve erro, o container já foi removido pelo helper. Os passos seguintes (readiness, cópia, restore, conexão) continuam removendo em caso de erro, como hoje. Cada engine verifica o ctx entre passos, para que o cancelamento vire erro em vez de avançar.
- A remoção por engine (`Remove`) já roda sem ctx. Continua assim.
- Redis mantém a limpeza que já faz dentro do closure. A remoção extra do helper é idempotente.

**Ctrl+C (`main.go`, módulo cli-ui, ponto de apoio necessário para a história 1)**
- O ctx do `run` passa a ser cancelado por SIGINT/SIGTERM **antes** do `Provision`. Durante o `Provision`, o sinal cancela o ctx e o `Provision` limpa e devolve erro. Depois do `Provision`, o comportamento atual se mantém: fecha a sessão, a menos que `--keep` esteja ativo, e sai com 130.
- A saída por cancelamento usa código 130 e uma mensagem curta de "interrompido".

**Checagem do Docker**
- `Available(ctx)` inclui no erro a saída do `docker info` (sem espaços nas pontas) e encadeia o erro original (`%w`). O erro de "docker não encontrado no PATH" também encadeia o original.
- O wrapper usado pelas engines recebe o ctx do `Provision` e aplica um prazo fixo (~10 s) sobre ele. As 5 engines Docker passam o ctx ao chamar o wrapper.
- As mensagens atuais continuam como prefixo, para que os testes por substring e os `t.Skip` existentes continuem funcionando.

**Heurística de coluna de ordenação (módulo relacional compartilhado)**
- Passam para o módulo relacional, com a lógica inalterada: a função de preferência por coluna (camadas 1–3 por nome, 4 para tipo de data, "não candidata" para o resto), a escolha da coluna (menor preferência vence; sem candidata, PK simples; sem PK, nenhuma) e a constante nomeada da camada de data.
- Tipo neutro de coluna (nome + tipo de dado) no módulo relacional. O tipo de coluna do MySQL vira um alias dele, para que os chamadores compilem sem mudança.
- O conjunto de tipos que contam como data é passado por cada dialeto. MySQL/MariaDB fornecem o conjunto atual. O SQLite fornece o próprio conjunto, inicialmente idêntico ao atual, para não mudar comportamento.
- Desempate dentro da mesma camada: vence a coluna de menor posição ordinal. Em Go isso já acontece, porque as colunas chegam em ordem ordinal e a comparação é estrita; a decisão passa a ser documentada e testada.
- Postgres: o `ORDER BY` do `DISTINCT ON` ganha a posição ordinal da coluna como critério depois da preferência. É a **única mudança de comportamento** deste item.
- O comentário do módulo relacional passa a listar todos os consumidores: Postgres (em SQL), MySQL/MariaDB e SQLite (em Go) e Mongo (só as camadas 1–3, achatadas).

**Helpers de teste da suíte de conformidade**
- Checar Docker disponível, checar se o container existe, gerar nome único e buscar coleção por nome saem do teste de fluxo do Postgres para um arquivo de helpers com a mesma build tag `docker`, ao lado da suíte. É uma movimentação pura, sem mudança de assinatura.

**Ordem sugerida dos commits** (cada um verde em `scripts/check fast`, e os que tocam Docker também em `full`):
1. Mover os helpers de teste da conformidade.
2. Diagnóstico e prazo do `Available`.
3. Limpeza em todo caminho de falha do retry + contrato no doc de `Provision`.
4. Verificação de ctx entre passos nas engines + Ctrl+C antes do `Provision` + subteste de conformidade de cancelamento.
5. Mover a heurística para o módulo relacional sem mudar comportamento.
6. Desempate ordinal no Postgres + caso de teste.

## Testing Decisions

Um bom teste aqui verifica comportamento externo pelo seam mais alto disponível: o que o `Provision` devolve e o que sobra no Docker, ou qual coluna a heurística escolhe para um conjunto de colunas. Não verifica a ordem interna de chamadas nem o texto exato de mensagens, salvo prefixos que já são contrato.

**Unitários (sem Docker, `scripts/check fast`), seam = `DockerHost` com `Run` falso (`recRun`)**
- Retry: um erro que não é conflito aborta **e** registra `rm -f <nome>`. O ctx cancelado antes da tentativa também registra a remoção. A remoção é chamada mesmo quando o ctx do chamador já está cancelado, isto é, o `Run` falso recebe um ctx não cancelado.
- `Available`: quando o `Run` falso devolve saída e erro, a mensagem contém a saída e o erro original é recuperável com `errors.Is`/`errors.As`.
- `Available`: com um `Run` bloqueante que só retorna quando o ctx termina e um ctx com prazo curto, retorna logo com erro de prazo.
- Referência existente: os testes `TestStartWithPortRetry_*` e `TestDockerHost_Available` do core.

**Unitários da heurística (sem Docker)**
- Empate na mesma camada → vence a menor posição ordinal. Camada 4 com tipos fornecidos pelo dialeto. Fallback para PK e para nenhuma coluna.
- Os testes existentes do MySQL e do SQLite sobre escolha de coluna continuam passando sem alteração: é essa a prova de que o comportamento foi preservado.
- Referência existente: os testes de `chooseOrderColumn` no módulo MySQL e os testes de sessão do SQLite.

**Conformidade (`-tags docker`, `scripts/check full`), seam = `Engine`/`Session` + callback `Progress`**
- Novo subteste genérico, sem ramificação por engine: primeiro mede `n`, o número de relatórios de `Progress` num `Provision` bem-sucedido do backup válido (pode ser o mesmo do subteste válido). Depois, para cada `k` em `1..n`, roda o `Provision` com um `Progress` que cancela o ctx no `k`-ésimo relatório e verifica: (a) o `Provision` devolve erro; (b) não existe container com o nome `db-verify-<pid>` (incluindo containers parados). Se por acaso vier uma `Session`, o teste a fecha e falha.
- Engines sem container (SQLite) passam trivialmente em (b), mas continuam obrigadas a (a).
- Custo aceito: cerca de 4 a 6 provisionamentos extras por engine.
- Referência existente: os subtestes "backup válido" e "backup truncado" da suíte de conformidade, e o `containerExists` que já é usado no subteste de `Close`.

**Postgres (`-tags docker`, teste de fluxo completo, tabela de camadas)**
- Nova tabela com `created_at` e `inserted_at` (ambos na camada 1, com `created_at` em posição ordinal menor) → a ordenação escolhida é `created_at`, e o hint indica data.
- Referência existente: o caso por camada da heurística no teste de fluxo completo do Postgres.

**Manual (não automatizado)**
- Ctrl+C durante o restore de um dump Postgres grande → o processo sai com 130 em poucos segundos e `docker ps -a` não mostra `db-verify-<pid>`.

## Out of Scope

- Migrar o ciclo de vida do container (run, cp, exec, espera de readiness, logs) para o `DockerHost` e injetar o host via `ProvisionOpts`. É a linha 10 do mapa, com direção já decidida, e fica para um spec próprio.
- Limite de "recentes" como constante do contrato (linha 8), correções de doc e remoção de `ExactCounts` (linha 7), guard contra empate na detecção e nome duplicado no registro (linha 5), e erro do `docker ps` no prompt de porta (linha 6).
- Mudar as janelas de porta, incluindo a sugestão de janelas altas para todas as engines (linha 9).
- Mongo: continua usando as camadas 1–3 achatadas. Dar a ele uma camada de data é outro trabalho.
- Credenciais fixas dos containers e o prompt que lista containers de terceiros: decididos como deliberados.
- Limpar containers deixados por execuções anteriores (outro pid).

## Further Notes

- O contrato de limpeza fala em "container ou outro recurso externo". O SQLite cria uma cópia temporária do arquivo, e pela regra ela também deve ser removida quando o `Provision` falha. A suíte só verifica containers. Se o SQLite hoje deixar o temporário, corrija no mesmo passo, mas nenhum teste novo é exigido para isso.
- `docker run -d` interrompido por cancelamento do cliente pode deixar o container no estado "Created". Por isso a verificação da conformidade precisa incluir containers parados, e o `rm -f` do helper precisa rodar com ctx próprio.
- Os comentários de código e as mensagens seguem em pt-BR, conforme o CLAUDE.md.
- Guardrails relacionados já registrados em `docs/tech-debt/README.md`: "Provision que falha não deixa container para trás" e "regras compartilhadas entre engines moram no core com nome".
