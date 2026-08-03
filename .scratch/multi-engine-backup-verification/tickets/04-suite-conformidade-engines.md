# 04 — Suíte de conformidade que toda engine precisa passar

**What to build:** um único corpo de teste, parametrizado por engine, que define
objetivamente o que significa "esta engine está pronta". Neste ticket ele roda só contra
Postgres; a partir daqui, cada engine nova é considerada pronta quando passa nele sem
nenhuma exceção específica.

Para cada engine, o teste gera um backup mínimo conhecido — duas coleções, uma com linhas
e uma vazia, uma com campo de data e uma sem — provisiona, e verifica o contrato inteiro.
Um caso negativo por engine usa um backup deliberadamente truncado e prova que os erros de
restore são reportados, não engolidos.

Exige Docker, então fica atrás de build tag: a camada de detecção, que concentra a lógica
que mais quebra, continua rodando em qualquer máquina sem Docker.

**Blocked by:** 02 — Extrair `Engine`/`Session` e reimplementar Postgres atrás da interface.

**Status:** ready-for-agent

- [ ] O corpo do teste é único e parametrizado pela lista de engines registradas, sem
      ramificação por nome de engine
- [ ] Cada engine declara como gerar seu backup de teste válido e seu backup truncado
- [ ] A suíte roda atrás de build tag e não é executada por um `go test` comum
- [ ] Contrato verificado: restore de backup válido reporta zero erros
- [ ] Contrato verificado: `Health` devolve nome e tamanho não vazios e nenhum contador
      negativo
- [ ] Contrato verificado: `Collections` devolve exatamente as coleções esperadas com
      contagem exata correta
- [ ] Contrato verificado: a coleção vazia aparece com contagem zero
- [ ] Contrato verificado: `Recent` devolve no máximo 20 linhas
- [ ] Contrato verificado: na coleção com campo de data, `Recent` devolve ordem decrescente
- [ ] Contrato verificado: `Query` com comando nativo válido devolve resultado, e com
      comando inválido devolve erro sem pânico
- [ ] Contrato verificado: `Close` remove o container, confirmado por inspeção do Docker
- [ ] Caso negativo: backup truncado produz erros de restore reportados, com log em arquivo
- [ ] Postgres passa a suíte inteira
