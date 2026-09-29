# 04: Ctrl+C e cancelamento no Provision limpam e saem

> **Difficulty:** Heavy: **suggested model:** Opus (Claude Code) / Opus ou o modelo de raciocínio mais forte disponível (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** o operador aperta Ctrl+C durante o provisionamento (espera de readiness, cópia, restore ou conexão). O db-verify cancela o trabalho em andamento, remove o container `db-verify-<pid>` e sai com código 130 e uma mensagem curta de "interrompido", em poucos segundos. Hoje o sinal só é tratado depois que o `Provision` retorna, e o container fica rodando com dados de produção.

Isso vale para toda engine. Cada engine Docker verifica o ctx entre os passos, para que o cancelamento vire erro em vez de seguir adiante, e remove o container em todo erro depois da subida (a subida em si já é coberta pelo ticket 03). O ctx do `run` passa a ser cancelado por SIGINT/SIGTERM **antes** do `Provision`. Depois do `Provision`, o comportamento atual se mantém: fecha a sessão, salvo com `--keep`, e sai com 130. Um `Provision` cancelado nunca devolve uma `Session` parcialmente pronta.

A suíte de conformidade prova a garantia para toda engine registrada, sem ramificação por nome. Primeiro mede `n`, o número de relatórios de `Progress` num `Provision` bem-sucedido. Depois, para cada `k` em `1..n`, roda um `Provision` cujo `Progress` cancela o ctx no `k`-ésimo relatório e verifica que ele devolve erro e que não existe container `db-verify-<pid>`, contando também os parados. Custo aceito: cerca de 4 a 6 provisionamentos extras por engine.

O SQLite não tem container: passa trivialmente na checagem de container, mas também tem que devolver erro quando cancelado. Se hoje ele deixa a cópia temporária para trás numa falha, corrija neste ticket (sem teste novo obrigatório).

Spec: `.scratch/core-debt-fase1/SPEC.md` (seções "Limpeza nas engines Docker", "Ctrl+C" e a conformidade em "Testing Decisions"). Linha 1 do mapa.

**Blocked by:** 01 (helpers da conformidade morando com a suíte), 03 (retry de porta remove o container em qualquer falha)

**Status:** ready-for-human

- [x] Existe na suíte de conformidade um subteste genérico de cancelamento em cada passo, e ele passa para Postgres, MySQL, MariaDB, SQLite, Redis e Mongo.
- [x] O subteste verifica também containers parados (estado "Created"/"Exited"), e não só os que estão rodando.
- [x] Se uma `Session` for devolvida apesar do cancelamento, o subteste a fecha e falha.
- [x] O sinal SIGINT/SIGTERM cancela o ctx do `Provision`; a saída por cancelamento usa código 130.
- [x] Verificação manual registrada nos comentários do ticket: Ctrl+C durante o restore de um dump Postgres grande → sai em poucos segundos e `docker ps -a` não mostra `db-verify-<pid>`.
- [x] `./scripts/check full` passa.

## Comentários

**Verificação manual (2026-09-29, agente):** dump Postgres `-Fc` de 204,5 MB (tabela de 8M linhas + 3 índices, gerado com `generate_series`). Binário lançado sob `setsid`; 3 s depois de "restaurando (pode demorar)…" (container `db-verify-<pid>` "Up"), `kill -INT -- -<pgid>` no grupo de processos (emula o Ctrl+C do terminal, que atinge também o `docker exec` filho). Resultado: "! interrompido", código 130, saída em ~0,6 s, `docker ps -a --filter name=db-verify-<pid>` vazio. Repetido com `kill -TERM <pid>` (só o processo): mesmo resultado. Não foi um Ctrl+C de teclado de verdade, e sim o sinal equivalente enviado ao grupo de processos.
