package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"db-verify/internal/docker"
	"db-verify/internal/engine"
)

// TestProvision_UsaHostInjetado prova que Provision fala com a CLI Docker só
// pelo Engine.Host: com um host falso, a subida do container falha e a
// limpeza (rm -f do nome fixo) passa pelo mesmo host — sem Docker de verdade.
func TestProvision_UsaHostInjetado(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	host := &docker.DockerHost{
		LookPath: func(string) (string, error) { return "/usr/bin/docker", nil },
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			mu.Lock()
			calls = append(calls, strings.Join(args, " "))
			mu.Unlock()
			if args[0] == "run" {
				return []byte("Unable to find image"), errors.New("exit status 125")
			}
			return nil, nil
		},
	}
	b := &engine.Backup{Path: "x.dump", Format: "custom", Compression: "none"}
	_, err := Engine{Host: host}.Provision(context.Background(), b, engine.ProvisionOpts{Port: 5555, DBName: "verify"})
	if err == nil || !strings.Contains(err.Error(), "Unable to find image") {
		t.Fatalf("erro: %v", err)
	}
	want := "rm -f " + docker.ContainerName()
	mu.Lock()
	defer mu.Unlock()
	if len(calls) == 0 || calls[0] != "info" {
		t.Fatalf("esperava docker info primeiro: %v", calls)
	}
	found := false
	for _, c := range calls {
		found = found || c == want
	}
	if !found {
		t.Fatalf("limpeza %q não passou pelo host: %v", want, calls)
	}
}
