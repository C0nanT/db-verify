# 06 — Engine MariaDB

**What to build:** backups gerados pelo MariaDB são reconhecidos como MariaDB — não como
MySQL — e restaurados na imagem certa. Do ponto de vista do usuário é mais uma engine na
lista; por baixo, a maior parte da implementação relacional é compartilhada com o MySQL.

MariaDB e MySQL são engines distintas no registry porque têm imagens e cabeçalhos
diferentes, e restaurar um dump MariaDB numa imagem MySQL pode falhar de formas sutis.
A separação existe para o operador não descobrir isso na hora errada.

**Blocked by:** 05 — Engine MySQL.

**Status:** ready-for-human

- [x] Um dump gerado pelo MariaDB é detectado como MariaDB, não como MySQL
- [x] Um dump gerado pelo MySQL continua sendo detectado como MySQL
- [x] O container usa a imagem do MariaDB, na versão inferida do dump quando disponível
- [x] A implementação relacional é compartilhada com o MySQL, sem duplicação da
      heurística de coluna nem das consultas de introspecção
- [x] `--engine mariadb` e `--engine mysql` forçam cada uma sua imagem
- [x] `--keep` imprime o comando de shell correto para MariaDB
- [x] A engine passa a suíte de conformidade sem nenhuma exceção específica
