# 08 — Engine SQLite

**What to build:** verificar um arquivo SQLite instantaneamente, sem Docker. É o caso que
prova que o provisionamento não precisa de container: quem está numa máquina sem Docker
continua com uma ferramenta útil em vez de ficar sem nada.

O "restore" é copiar o arquivo para um temporário e rodar uma checagem de integridade,
cujo resultado alimenta os erros de restore exibidos como em qualquer outra engine. A
cópia existe para a verificação nunca escrever no arquivo original do usuário.

**Blocked by:** 03 — Detecção por registry, flags e picker multi-formato; 04 — Suíte de
conformidade que toda engine precisa passar.

**Status:** ready-for-human

- [x] Um arquivo SQLite é detectado pelo magic `SQLite format 3`, também comprimido
- [x] Extensões `.sqlite` e `.db` contam como sinal de confiança média
- [x] A verificação não sobe container nenhum e é instantânea
- [x] Verificar um SQLite funciona numa máquina sem Docker; as engines que exigem
      container é que falham, com erro claro
- [x] O arquivo original do usuário nunca é modificado — a verificação roda sobre uma cópia
- [x] A checagem de integridade roda, e um arquivo corrompido produz erro de restore
      reportado como nas outras engines
- [x] Saúde mostra os contadores que fazem sentido para SQLite, sem campos zerados
      inaplicáveis
- [x] Coleções são as tabelas do arquivo, com contagem e tamanho
- [x] A coluna de ordenação segue a mesma heurística das demais engines relacionais
- [x] O SQL executado aparece na tela, copiável para o cliente `sqlite3`
- [x] O temporário é removido ao sair, e `--keep` o preserva imprimindo seu caminho
- [x] A engine passa a suíte de conformidade sem nenhuma exceção específica
