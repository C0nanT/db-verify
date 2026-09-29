# 05: Heurística de ordenação inteira no módulo relacional

> **Difficulty:** Standard: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** quem mantém o projeto passa a mudar a regra de "coluna de ordenação" num lugar só. A função de preferência por coluna (camadas 1–3 por nome, camada de data com constante nomeada, "não candidata" para o resto) e a escolha da coluna (menor preferência vence; sem candidata, PK simples; sem PK, nenhuma) passam do MySQL para o módulo relacional compartilhado, **sem mudar comportamento**.

A heurística trabalha sobre um tipo de coluna neutro (nome + tipo de dado); o tipo de coluna do MySQL vira alias dele, para que os chamadores compilem sem mudança. Cada dialeto informa o próprio conjunto de tipos de data: MySQL/MariaDB o atual, e o SQLite um conjunto próprio, inicialmente idêntico. O SQLite deixa de depender de tipos do MySQL. O desempate dentro da mesma camada (vence a menor posição ordinal) fica documentado e testado. O comentário do módulo relacional passa a listar todos os consumidores: Postgres (em SQL), MySQL/MariaDB e SQLite (em Go) e Mongo (só as camadas 1–3, achatadas).

Spec: `.scratch/core-debt-fase1/SPEC.md` (seção "Heurística de coluna de ordenação"). Linha 2 do mapa.

**Blocked by:** None (can start immediately)

**Status:** ready-for-human

- [x] A heurística (preferência, escolha e constante da camada de data) está definida no módulo relacional; o MySQL e o SQLite a consomem de lá.
- [x] O SQLite não referencia mais nenhum tipo nem função do MySQL para escolher a coluna de ordenação.
- [x] Teste unitário novo: empate na mesma camada → vence a menor posição ordinal; camada de data com tipos informados pelo dialeto; fallback para PK; nenhuma coluna.
- [x] Os testes existentes do MySQL e do SQLite sobre escolha de coluna passam **sem alteração**.
- [x] O comentário do módulo relacional lista todos os consumidores.
- [x] `./scripts/check fast` passa.
