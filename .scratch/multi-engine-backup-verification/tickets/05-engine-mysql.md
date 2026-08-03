# 05 — Engine MySQL

**What to build:** apontar o `db-verify` para um `mysqldump` e ter a mesma experiência de
sempre: engine detectada sozinha, container na versão certa, restore, e a TUI com tabelas
à esquerda e os 20 registros mais recentes à direita.

Esta é a primeira engine além do Postgres, então é ela que prova que o seam funciona.
Ela reusa a lista compartilhada de nomes de coluna de ordenação extraída no ticket 02 —
incluindo os nomes em português — traduzida para o dialeto de `information_schema` do
MySQL. A heurística vista pelo usuário é a mesma do Postgres.

**Blocked by:** 03 — Detecção por registry, flags e picker multi-formato; 04 — Suíte de
conformidade que toda engine precisa passar.

**Status:** ready-for-agent

- [ ] Um `mysqldump` é detectado pelo cabeçalho, e também quando comprimido
- [ ] Extensão `.sql` conta como sinal de confiança média para a detecção
- [ ] A versão da imagem é inferida do cabeçalho do dump quando ele a informa, com
      fallback documentado quando não
- [ ] O container sobe com durabilidade relaxada, por ser dado descartável
- [ ] A porta no host parte da porta padrão do MySQL e escolhe a primeira livre
- [ ] O backup é transmitido para dentro do container por stream, sem duplicar em disco
- [ ] Erros do restore de MySQL são reconhecidos como erros e reportados; um restore
      limpo não reporta erro falso
- [ ] Saúde mostra tabelas, views, índices, chaves estrangeiras e também procedures e
      triggers
- [ ] Coleções são listadas como database.tabela, com contagem e tamanho
- [ ] Contagem exata e estimada são suportadas, mapeando para o equivalente do MySQL
- [ ] A coluna de ordenação segue a mesma heurística do Postgres, com os mesmos nomes em
      português suportados
- [ ] O painel mostra o SQL nativo executado, copiável para um cliente MySQL
- [ ] `--keep` imprime o comando `mysql` para reconectar ao container preservado
- [ ] A engine passa a suíte de conformidade sem nenhuma exceção específica
