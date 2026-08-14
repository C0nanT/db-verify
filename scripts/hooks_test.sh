#!/usr/bin/env bash
# Smoke test dos hooks versionados e do setup. Isolado em clones temporários.
# Uso: scripts/hooks_test.sh

set -uo pipefail

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPTS_DIR/.." && pwd)"
CHECK_SCRIPT="$SCRIPTS_DIR/check"
INSTALL_HOOKS="$SCRIPTS_DIR/install-hooks"
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
    mkdir -p scripts .githooks
    cp "$CHECK_SCRIPT" scripts/check
    cp "$INSTALL_HOOKS" scripts/install-hooks
    cp "$REPO_ROOT/.githooks/pre-commit" .githooks/pre-commit
    cp "$REPO_ROOT/.githooks/pre-push" .githooks/pre-push
    chmod +x scripts/check scripts/install-hooks .githooks/pre-commit .githooks/pre-push
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

# Cenário 1: setup aponta hooksPath local; config global inalterada
repo="$(new_fixture_repo)"
global_before="$(git config --global --get core.hooksPath || true)"
out="$(cd "$repo" && ./scripts/install-hooks 2>&1)"
code=$?
local_hooks="$(cd "$repo" && git config --local --get core.hooksPath)"
global_after="$(git config --global --get core.hooksPath || true)"
assert_eq "install-hooks -> exit 0" "0" "$code"
assert_eq "hooksPath local = .githooks" ".githooks" "$local_hooks"
assert_eq "config global inalterada" "$global_before" "$global_after"
if [[ "$out" == *"global"* && "$out" == *"git config --global"* ]]; then
  echo "FALHOU: setup não deveria invocar git config --global"
  failures=$((failures + 1))
else
  echo "ok: setup não usa git config --global"
fi
rm -rf "$repo"

# Cenário 2: commit com formatação quebrada aborta
repo="$(new_fixture_repo)"
cat > "$repo/bad.go" <<'EOF'
package main
func Bad() {
      return
}
EOF
(
  cd "$repo"
  ./scripts/install-hooks >/dev/null
  git add -A
)
out="$(cd "$repo" && git commit -m 'quebrado' 2>&1)"
code=$?
assert_eq "commit desformatado -> exit != 0" "1" "$([[ $code -ne 0 ]] && echo 1 || echo 0)"
assert_contains "commit desformatado menciona bad.go" "$out" "bad.go"
assert_contains "commit desformatado sugere gofmt -w" "$out" "gofmt -w"
rm -rf "$repo"

# Cenário 3: commit com árvore limpa passa
repo="$(new_fixture_repo)"
(
  cd "$repo"
  ./scripts/install-hooks >/dev/null
  git add -A
)
out="$(cd "$repo" && git commit -m 'limpo' 2>&1)"
code=$?
assert_eq "commit limpo -> exit 0" "0" "$code"
rm -rf "$repo"

# Cenário 4: gate ignora o que está staged — untracked/unstaged sujo ainda aborta
repo="$(new_fixture_repo)"
cat > "$repo/README.md" <<'EOF'
ok
EOF
cat > "$repo/bad.go" <<'EOF'
package main
func Bad() {
      return
}
EOF
(
  cd "$repo"
  ./scripts/install-hooks >/dev/null
  git add README.md
)
out="$(cd "$repo" && git commit -m 'só docs' 2>&1)"
code=$?
assert_eq "commit com bad.go fora do stage -> exit != 0" "1" "$([[ $code -ne 0 ]] && echo 1 || echo 0)"
assert_contains "ainda vê bad.go" "$out" "bad.go"
rm -rf "$repo"

# Cenário 5: hook a partir de subdiretório
repo="$(new_fixture_repo)"
mkdir -p "$repo/sub/dir"
(
  cd "$repo"
  ./scripts/install-hooks >/dev/null
  git add -A
)
out="$(cd "$repo/sub/dir" && git commit -m 'de subdir' 2>&1)"
code=$?
assert_eq "commit a partir de subdiretório -> exit 0" "0" "$code"
rm -rf "$repo"

# Cenário 6: push com Docker parado aborta (mensagem do daemon)
repo="$(new_fixture_repo)"
bare="$(mktemp -d)"
git init -q --bare "$bare"
fake_bin="$(fake_docker_bin 1)"
(
  cd "$repo"
  git remote add origin "$bare"
  git add -A
  git commit --no-verify -q -m 'base'
  ./scripts/install-hooks >/dev/null
)
out="$(cd "$repo" && PATH="$fake_bin:$PATH" git push origin HEAD 2>&1)"
code=$?
assert_eq "push com Docker parado -> exit != 0" "1" "$([[ $code -ne 0 ]] && echo 1 || echo 0)"
assert_contains "push menciona Docker parado" "$out" "Docker não respondeu"
rm -rf "$repo" "$bare" "$fake_bin"

# Cenário 7: push com suite verde (Docker fake ok) passa
repo="$(new_fixture_repo)"
bare="$(mktemp -d)"
git init -q --bare "$bare"
fake_bin="$(fake_docker_bin 0)"
(
  cd "$repo"
  git remote add origin "$bare"
  git add -A
  git commit --no-verify -q -m 'base'
  ./scripts/install-hooks >/dev/null
)
out="$(cd "$repo" && PATH="$fake_bin:$PATH" git push origin HEAD 2>&1)"
code=$?
assert_eq "push com Docker ok -> exit 0" "0" "$code"
rm -rf "$repo" "$bare" "$fake_bin"

# Cenário 8: hooks não redeclaram comandos de qualidade
pre_commit="$(cat "$REPO_ROOT/.githooks/pre-commit")"
pre_push="$(cat "$REPO_ROOT/.githooks/pre-push")"
if [[ "$pre_commit" == *"gofmt"* || "$pre_commit" == *"golangci-lint"* || "$pre_commit" == *"gitleaks"* || "$pre_commit" == *"go test"* || "$pre_commit" == *"go vet"* ]]; then
  echo "FALHOU: pre-commit duplica comandos de qualidade"
  failures=$((failures + 1))
else
  echo "ok: pre-commit só dispara o ponto de entrada"
fi
if [[ "$pre_push" == *"gofmt"* || "$pre_push" == *"golangci-lint"* || "$pre_push" == *"gitleaks"* || "$pre_push" == *"go test"* || "$pre_push" == *"docker info"* ]]; then
  echo "FALHOU: pre-push duplica comandos de qualidade"
  failures=$((failures + 1))
else
  echo "ok: pre-push só dispara o ponto de entrada"
fi
if [[ "$pre_commit" != *"scripts/check"* || "$pre_commit" != *"fast"* ]]; then
  echo "FALHOU: pre-commit deveria invocar scripts/check fast"
  failures=$((failures + 1))
else
  echo "ok: pre-commit invoca scripts/check fast"
fi
if [[ "$pre_push" != *"scripts/check"* || "$pre_push" != *"full"* ]]; then
  echo "FALHOU: pre-push deveria invocar scripts/check full"
  failures=$((failures + 1))
else
  echo "ok: pre-push invoca scripts/check full"
fi

echo
if [[ "$failures" -eq 0 ]]; then
  echo "Todos os cenários passaram."
  exit 0
else
  echo "$failures cenário(s) falharam."
  exit 1
fi
