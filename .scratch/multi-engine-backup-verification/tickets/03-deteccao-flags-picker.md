# 03 — Detecção por registry, flags de engine e picker multi-formato

**What to build:** o usuário passa a poder apontar para um arquivo de backup de qualquer
tipo e a ferramenta decide sozinha qual engine usar — ou aceita ser corrigida por flag.
Com só Postgres registrado o comportamento continua idêntico; o que este ticket entrega é
a infraestrutura de escolha, provada ponta a ponta antes de existir uma segunda engine.

A detecção tem duas fases: abrir o arquivo e descomprimir o cabeçalho se preciso, depois
perguntar a cada engine registrada e ficar com a de maior confiança. Magic bytes valem
mais que extensão, que vale mais que palpite. Empate resolve por ordem de registro.
`--engine` pula a fase inteira.

O picker de `data/` deixa de filtrar por `.dump` e passa a listar todo arquivo que alguma
engine reconheça, com uma coluna nova mostrando qual. Arquivos que ninguém reconhece
aparecem marcados como desconhecidos em vez de sumirem — o operador precisa saber que
existe algo ali que a ferramenta não entende.

**Blocked by:** 02 — Extrair `Engine`/`Session` e reimplementar Postgres atrás da interface.

**Status:** ready-for-agent

- [ ] A detecção descomprime o cabeçalho antes de identificar, cobrindo gzip, zstd e bzip2
- [ ] Um backup Postgres gzipado, zstdado e bzipado é detectado corretamente pelos três
- [ ] A detecção lê apenas o cabeçalho — apontar para um arquivo enorme não trava o programa
- [ ] Magic bytes vencem extensão, e extensão vence palpite, quando há conflito
- [ ] `--engine <nome>` força a engine e pula a detecção
- [ ] `--engine` com nome inexistente falha com erro que lista as engines disponíveis
- [ ] `--list-engines` enumera as engines suportadas e sai
- [ ] Um arquivo que nenhuma engine reconhece falha com erro que nomeia as engines
      disponíveis e o que cada uma espera
- [ ] Um `.sql` plain sem cabeçalho identificável cai em Postgres com aviso explícito e
      sugestão de usar `--engine`
- [ ] `--version-tag` define a versão da imagem para qualquer engine
- [ ] `--pg` continua funcionando como alias de `--version-tag`, imprimindo aviso de
      depreciação, sem quebrar scripts existentes
- [ ] O picker lista todo arquivo reconhecido em `data/`, não só `.dump`
- [ ] O picker mostra a engine detectada de cada arquivo
- [ ] Arquivos não reconhecidos aparecem no picker marcados como desconhecidos e não são
      selecionáveis para verificação
- [ ] A suíte do ticket 01 continua verde
