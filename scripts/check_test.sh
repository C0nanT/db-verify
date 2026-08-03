#!/usr/bin/env bash
# Smoke test do scripts/check. Roda cenários em repositórios git temporários e isolados
# (não toca a árvore real do projeto). Uso: scripts/check_test.sh
#
# Cada cenário monta um módulo Go mínimo, roda scripts/check e valida exit code + saída.

set -uo pipefail

CHECK_SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check"
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

new_fixture_repo() {
  local dir
  dir="$(mktemp -d)"
  (
    cd "$dir"
    git init -q
    git config user.email test@example.com
    git config user.name test
    local go_version
    go_version="$(go env GOVERSION | sed 's/^go//')"
    cat > go.mod <<EOF
module fixture

go ${go_version}
EOF
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

echo
if [[ "$failures" -eq 0 ]]; then
  echo "Todos os cenários passaram."
  exit 0
else
  echo "$failures cenário(s) falharam."
  exit 1
fi
