#!/usr/bin/env bash
# Smoke test do workflow GitHub Actions. Valida gatilhos, jobs e que o YAML só
# chama o ponto de entrada versionado. Uso: scripts/workflow_test.sh

set -uo pipefail

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPTS_DIR/.." && pwd)"
WF="$REPO_ROOT/.github/workflows/quality.yml"
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

assert_not_contains() {
  local desc="$1" haystack="$2" needle="$3"
  if [[ "$haystack" == *"$needle"* ]]; then
    echo "FALHOU: $desc (não deveria conter '$needle')"
    failures=$((failures + 1))
  else
    echo "ok: $desc"
  fi
}

if [[ ! -f "$WF" ]]; then
  echo "FALHOU: workflow ausente em .github/workflows/quality.yml"
  exit 1
fi

yaml="$(cat "$WF")"

assert_contains "dispara em push" "$yaml" "push:"
assert_contains "push para main" "$yaml" "main"
assert_contains "dispara em pull_request" "$yaml" "pull_request"
assert_contains "acionamento manual" "$yaml" "workflow_dispatch"

# Dois jobs distintos no relatório do PR (chaves de 2 espaços sob `jobs:`).
job_ids="$(awk '/^jobs:/{p=1;next} p && /^  [A-Za-z0-9_-]+:/{print $1}' "$WF" | tr -d ':')"
job_count="$(printf '%s\n' "$job_ids" | grep -c . || true)"
assert_eq "dois jobs no workflow" "2" "$job_count"

assert_contains "job rápido chama scripts/check ci" "$yaml" "scripts/check ci"
assert_contains "job Docker chama scripts/check docker" "$yaml" "scripts/check docker"

# Nenhum comando de qualidade redeclarado no YAML (além do ponto de entrada).
for cmd in "gofmt" "go vet" "golangci-lint" "gitleaks" "govulncheck" "go test" "go mod tidy" "go build"; do
  # Permite menções em comentários? Ticket: nenhum comando redeclarado. Grep run: lines.
  if grep -E '^\s+run:' "$WF" | grep -F "$cmd" >/dev/null; then
    echo "FALHOU: comando de qualidade redeclarado no YAML: $cmd"
    failures=$((failures + 1))
  else
    echo "ok: YAML não redeclara $cmd"
  fi
done

assert_contains "Go derivado do go.mod" "$yaml" "go-version-file:"
assert_contains "go-version-file aponta para go.mod" "$yaml" "go.mod"
assert_contains "cache de módulos/build" "$yaml" "cache:"
assert_contains "timeout no job pesado" "$yaml" "timeout-minutes:"

echo
if [[ "$failures" -eq 0 ]]; then
  echo "Todos os cenários passaram."
  exit 0
else
  echo "$failures cenário(s) falharam."
  exit 1
fi
