//go:build docker

package main

// Helpers compartilhados pela suíte de conformidade (conformance_test.go e
// os *_conformance_test.go de cada engine). Ficam aqui, ao lado da suíte,
// para ela compilar sem depender do teste de fluxo completo do Postgres.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"db-verify/internal/docker"
)

func requireDocker(t *testing.T) {
	t.Helper()
	if err := docker.DockerAvailable(context.Background()); err != nil {
		t.Skipf("docker indisponível: %v", err)
	}
}

func containerExists(name string) bool {
	return exec.Command("docker", "inspect", name).Run() == nil
}

func uniqueName(part string) string {
	return fmt.Sprintf("db-verify-test-%s-%d-%d", part, os.Getpid(), time.Now().UnixNano())
}

// collectionByName é um helper de busca linear — a lista de Collections não
// é grande o bastante para justificar um índice.
func collectionByName(collections []Collection, name string) (Collection, bool) {
	for _, c := range collections {
		if c.Name == name {
			return c, true
		}
	}
	return Collection{}, false
}
