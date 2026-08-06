#!/usr/bin/env bash
# Smoke test do scripts/check. Roda cenários em repositórios git temporários e isolados
# (não toca a árvore real do projeto). Uso: scripts/check_test.sh
#
# Cada cenário monta um módulo Go mínimo, roda scripts/check e valida exit code + saída.

set -uo pipefail

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK_SCRIPT="$SCRIPTS_DIR/check"
REPO_ROOT="$(cd "$SCRIPTS_DIR/.." && pwd)"
failures=0

assert_eq() {
  local desc="$1" expected="$2" actual="$3"
  if [[ "$expected" != "$actual" ]]; then
    echo "FALHOU: $desc (esperado='$expected' obtido='$actual')"
    failures=$((failures + 1))
  else
    echo "ok: $desc"
  fi
}

assert_contains() {
  local desc="$1" haystack="$2" needle="$3"
  if [[ "$haystack" != *"$needle"* ]]; then
    echo "FALHOU: $desc (saída não contém '$needle')"
    failures=$((failures + 1))
  else
    echo "ok: $desc"
  fi
}

assert_last_line() {
  local desc="$1" haystack="$2" needle="$3"
  local last_line
  last_line="$(echo "$haystack" | tail -n1)"
  if [[ "$last_line" != *"$needle"* ]]; then
    echo "FALHOU: $desc (última linha='$last_line', esperado conter '$needle')"
    failures=$((failures + 1))
  else
    echo "ok: $desc"
  fi
}

# Reaproveita go.mod/go.sum do repo real (mesma ferramenta pinada via tool dependency,
# já presente no cache de módulos) em vez de recalcular versão/dependências do zero —
# garante que o fixture roda o gate de lint com a mesma versão do golangci-lint.
new_fixture_repo() {
  local dir
  dir="$(mktemp -d)"
  (
    cd "$dir"
    git init -q
    git config user.email test@example.com
    git config user.name test
    sed '1s#^module .*#module fixture#' "$REPO_ROOT/go.mod" > go.mod
    cp "$REPO_ROOT/go.sum" go.sum
    cp "$REPO_ROOT/.golangci.yml" .golangci.yml
    cp "$REPO_ROOT/.gitleaks.toml" .gitleaks.toml
    cat > main.go <<'EOF'
package main

func main() {}
EOF
    cat > main_test.go <<'EOF'
package main

import "testing"

func TestOK(t *testing.T) {}
EOF
  )
  echo "$dir"
}

# Cenário 1: árvore saudável -> exit 0
repo="$(new_fixture_repo)"
out="$(cd "$repo" && "$CHECK_SCRIPT" fast 2>&1)"
code=$?
assert_eq "árvore saudável -> exit 0" "0" "$code"
rm -rf "$repo"

# Cenário 2: arquivo desformatado -> exit != 0, lista arquivo e gofmt -w
repo="$(new_fixture_repo)"
cat > "$repo/bad.go" <<'EOF'
package main
func Bad() {
      return
}
EOF
out="$(cd "$repo" && "$CHECK_SCRIPT" fast 2>&1)"
code=$?
assert_eq "arquivo desformatado -> exit != 0" "1" "$([[ $code -ne 0 ]] && echo 1 || echo 0)"
assert_contains "menciona bad.go" "$out" "bad.go"
assert_contains "sugere gofmt -w" "$out" "gofmt -w"
assert_last_line "última linha identifica etapa format" "$out" "format"
rm -rf "$repo"

# Cenário 2b: go vet falha (Printf com formato incompatível) -> exit != 0, etapa vet, para antes do unit
repo="$(new_fixture_repo)"
cat > "$repo/main.go" <<'EOF'
package main

import "fmt"

func main() {
	fmt.Printf("%d\n", "não é um número")
}
EOF
out="$(cd "$repo" && "$CHECK_SCRIPT" fast 2>&1)"
code=$?
assert_eq "go vet falha -> exit != 0" "1" "$([[ $code -ne 0 ]] && echo 1 || echo 0)"
assert_last_line "última linha identifica etapa vet" "$out" "vet"
if [[ "$out" == *"testes unitários"* ]]; then
  echo "FALHOU: falha em vet não deveria chegar à etapa de testes unitários"
  failures=$((failures + 1))
else
  echo "ok: falha em vet não chega à etapa de testes unitários"
fi
rm -rf "$repo"

# Cenário 2c: achado de lint (erro de retorno ignorado) -> exit != 0, etapa lint, para antes do unit
repo="$(new_fixture_repo)"
cat > "$repo/main.go" <<'EOF'
package main

import "os"

func main() {
	os.Open("main.go")
}
EOF
out="$(cd "$repo" && "$CHECK_SCRIPT" fast 2>&1)"
code=$?
assert_eq "achado de lint -> exit != 0" "1" "$([[ $code -ne 0 ]] && echo 1 || echo 0)"
assert_last_line "última linha identifica etapa lint" "$out" "lint"
if [[ "$out" == *"testes unitários"* ]]; then
  echo "FALHOU: falha em lint não deveria chegar à etapa de testes unitários"
  failures=$((failures + 1))
else
  echo "ok: falha em lint não chega à etapa de testes unitários"
fi
rm -rf "$repo"

# Cenário 2d: segredo plantado e staged -> exit != 0, etapa secrets, para antes do unit
repo="$(new_fixture_repo)"
# AKIA… com entropia suficiente; o exemplo oficial da AWS é allowlistado pelo gitleaks.
printf '%s\n' 'aws_access_key_id = AKIAJG74V2RRT4XVRMSA' > "$repo/planted-secret.env"
(
  cd "$repo"
  git add planted-secret.env
)
out="$(cd "$repo" && "$CHECK_SCRIPT" fast 2>&1)"
code=$?
assert_eq "segredo staged -> exit != 0" "1" "$([[ $code -ne 0 ]] && echo 1 || echo 0)"
assert_last_line "última linha identifica etapa secrets" "$out" "secrets"
if [[ "$out" == *"testes unitários"* ]]; then
  echo "FALHOU: falha em secrets não deveria chegar à etapa de testes unitários"
  failures=$((failures + 1))
else
  echo "ok: falha em secrets não chega à etapa de testes unitários"
fi
rm -rf "$repo"

# Cenário 3: teste unitário quebrado -> exit != 0, etapa unit
repo="$(new_fixture_repo)"
cat > "$repo/main_test.go" <<'EOF'
package main

import "testing"

func TestBroken(t *testing.T) {
	t.Fatal("quebrado de propósito")
}
EOF
out="$(cd "$repo" && "$CHECK_SCRIPT" fast 2>&1)"
code=$?
assert_eq "teste quebrado -> exit != 0" "1" "$([[ $code -ne 0 ]] && echo 1 || echo 0)"
assert_last_line "última linha identifica etapa unit" "$out" "unit"
assert_contains "saída bruta do go test visível" "$out" "quebrado de propósito"
rm -rf "$repo"

# Cenário 4: nível ausente -> erro pt-BR listando níveis válidos
out="$("$CHECK_SCRIPT" 2>&1)"
code=$?
assert_eq "nível ausente -> exit != 0" "1" "$([[ $code -ne 0 ]] && echo 1 || echo 0)"
assert_contains "lista níveis válidos" "$out" "Níveis válidos"

# Cenário 5: nível desconhecido -> erro pt-BR listando níveis válidos
out="$("$CHECK_SCRIPT" bogus 2>&1)"
code=$?
assert_eq "nível desconhecido -> exit != 0" "1" "$([[ $code -ne 0 ]] && echo 1 || echo 0)"
assert_contains "lista níveis válidos (desconhecido)" "$out" "Níveis válidos"

# Cenário 6: invocado a partir de subdiretório -> mesmo resultado
repo="$(new_fixture_repo)"
mkdir -p "$repo/sub/dir"
out="$(cd "$repo/sub/dir" && "$CHECK_SCRIPT" fast 2>&1)"
code=$?
assert_eq "subdiretório -> exit 0" "0" "$code"
rm -rf "$repo"

# Fabrica um `docker` fake no PATH, para exercitar o preflight sem depender de um
# daemon real. $1 = exit code que `docker info` deve devolver.
fake_docker_bin() {
  local exit_code="$1" dir
  dir="$(mktemp -d)"
  cat > "$dir/docker" <<EOF
#!/usr/bin/env bash
if [[ "\$1" == "info" ]]; then
  exit ${exit_code}
fi
exit 0
EOF
  chmod +x "$dir/docker"
  echo "$dir"
}

# Cenário 7: full com Docker parado -> exit != 0, etapa docker, mensagem em pt-BR,
# suite pesada não roda (sem "testes unitários" repetido nem saída de go test -tags docker)
repo="$(new_fixture_repo)"
fake_bin="$(fake_docker_bin 1)"
out="$(cd "$repo" && PATH="$fake_bin:$PATH" "$CHECK_SCRIPT" full 2>&1)"
code=$?
assert_eq "full com Docker parado -> exit != 0" "1" "$([[ $code -ne 0 ]] && echo 1 || echo 0)"
assert_contains "menciona Docker parado" "$out" "Docker não respondeu"
assert_last_line "última linha identifica etapa docker" "$out" "docker"
if [[ "$out" == *"suite de conformidade"* ]]; then
  echo "FALHOU: Docker parado não deveria iniciar a suite de conformidade"
  failures=$((failures + 1))
else
  echo "ok: Docker parado não inicia a suite de conformidade"
fi
rm -rf "$repo" "$fake_bin"

# Cenário 8: full com árvore saudável e Docker respondendo -> exit 0
repo="$(new_fixture_repo)"
fake_bin="$(fake_docker_bin 0)"
out="$(cd "$repo" && PATH="$fake_bin:$PATH" "$CHECK_SCRIPT" full 2>&1)"
code=$?
assert_eq "full com Docker ok -> exit 0" "0" "$code"
rm -rf "$repo" "$fake_bin"

# Cenário 9: falha no nível fast (formatação) com `full` -> aborta antes do preflight Docker,
# nenhum container sobe
repo="$(new_fixture_repo)"
cat > "$repo/bad.go" <<'EOF'
package main
func Bad() {
      return
}
EOF
fake_bin="$(fake_docker_bin 0)"
out="$(cd "$repo" && PATH="$fake_bin:$PATH" "$CHECK_SCRIPT" full 2>&1)"
code=$?
assert_eq "full com fast quebrado -> exit != 0" "1" "$([[ $code -ne 0 ]] && echo 1 || echo 0)"
assert_last_line "última linha identifica etapa format, não docker" "$out" "format"
if [[ "$out" == *"preflight do Docker"* ]]; then
  echo "FALHOU: falha no nível fast não deveria chegar ao preflight do Docker"
  failures=$((failures + 1))
else
  echo "ok: falha no nível fast aborta antes do preflight do Docker"
fi
rm -rf "$repo" "$fake_bin"

echo
if [[ "$failures" -eq 0 ]]; then
  echo "Todos os cenários passaram."
  exit 0
else
  echo "$failures cenário(s) falharam."
  exit 1
fi
