# 09 — Engine MongoDB

**What to build:** verificar um arquivo de `mongodump --archive` com o mesmo fluxo das
demais engines. O desafio é caber documentos aninhados num painel que é uma tabela: campos
são achatados com caminho pontilhado (`endereco.cidade`) até uma profundidade fixa, e
arrays viram JSON compacto.

Sem campo de data óbvio, a ordenação é por `_id` decrescente — o ObjectId carrega o
timestamp de criação, então isso entrega de fato os documentos mais recentes.

**Blocked by:** 03 — Detecção por registry, flags e picker multi-formato; 04 — Suíte de
conformidade que toda engine precisa passar.

**Status:** ready-for-agent

- [ ] Um arquivo de `mongodump --archive` é detectado pelo cabeçalho, também comprimido
- [ ] Extensão `.archive` conta como sinal de confiança média
- [ ] O restore usa `mongorestore --archive` lendo de stdin, sem arquivo temporário
      dentro do container
- [ ] O timeout de "ficar pronto" é específico do Mongo, que demora mais a subir que os demais
- [ ] Erros do `mongorestore` são reconhecidos e reportados, com log em arquivo
- [ ] Saúde mostra contagem de coleções, índices e tamanho total
- [ ] Coleções são listadas como db.coleção, com contagem de documentos e tamanho
- [ ] Contagem exata e estimada mapeiam para os comandos equivalentes do Mongo
- [ ] Documentos aninhados são achatados com caminho pontilhado até profundidade fixa
- [ ] Arrays são renderizados como JSON compacto e não quebram o alinhamento das colunas
- [ ] Recentes ordena por campo de data quando existe, e por `_id` decrescente quando não
- [ ] O comando Mongo nativo aparece na tela, copiável para o `mongosh`
- [ ] `--keep` imprime o comando `mongosh` para reconectar
- [ ] A engine passa a suíte de conformidade sem nenhuma exceção específica
