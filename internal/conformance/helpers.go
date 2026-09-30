//go:build docker

package conformance

// Helpers compartilhados pela suíte de conformidade, pelas fixtures de cada
// engine e pelo teste de fluxo completo do Postgres.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"db-verify/internal/docker"
	"db-verify/internal/engine"
)

// RequireDocker pula o teste quando o daemon Docker não está disponível.
func RequireDocker(t *testing.T) {
	t.Helper()
	if err := docker.DockerAvailable(context.Background()); err != nil {
		t.Skipf("docker indisponível: %v", err)
	}
}

// ContainerExists diz se existe um container com esse nome, em qualquer
// estado (Created/Exited inclusive, não só rodando): usa `docker inspect`.
func ContainerExists(name string) bool {
	return exec.Command("docker", "inspect", name).Run() == nil
}

// UniqueName gera um nome de container único por processo e instante, para
// testes paralelos ou repetidos não disputarem o mesmo nome.
func UniqueName(part string) string {
	return fmt.Sprintf("db-verify-test-%s-%d-%d", part, os.Getpid(), time.Now().UnixNano())
}

// CollectionByName é um helper de busca linear — a lista de Collections não
// é grande o bastante para justificar um índice.
func CollectionByName(collections []engine.Collection, name string) (engine.Collection, bool) {
	for _, c := range collections {
		if c.Name == name {
			return c, true
		}
	}
	return engine.Collection{}, false
}
