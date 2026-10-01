package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIsPortConflict(t *testing.T) {
	t.Parallel()
	yes := []string{
		"Error: port is already allocated",
		"bind: address already in use",
		"Bind for 0.0.0.0:5432 failed: port is already allocated",
		"driver failed programming external connectivity on endpoint: Bind for 127.0.0.1:3306 failed",
	}
	for _, msg := range yes {
		if !isPortConflict(msg) {
			t.Errorf("queria conflito: %q", msg)
		}
	}
	no := []string{
		"Unable to find image 'postgres:99' locally",
		"Cannot connect to the Docker daemon",
		"permission denied",
		"",
	}
	for _, msg := range no {
		if isPortConflict(msg) {
			t.Errorf("não queria conflito: %q", msg)
		}
	}
}

type recRun struct {
	mu    sync.Mutex
	calls [][]string
	fn    dockerRun
}

func (r *recRun) Run(ctx context.Context, args ...string) ([]byte, error) {
	r.mu.Lock()
	cp := append([]string(nil), args...)
	r.calls = append(r.calls, cp)
	r.mu.Unlock()
	if r.fn != nil {
		return r.fn(ctx, args...)
	}
	return nil, nil
}

func (r *recRun) has(prefix ...string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if len(c) < len(prefix) {
			continue
		}
		ok := true
		for i := range prefix {
			if c[i] != prefix[i] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func (r *recRun) countPrefix(first string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if len(c) > 0 && c[0] == first {
			n++
		}
	}
	return n
}

func conflictErr() error {
	return errors.New("Error response from daemon: Bind for 127.0.0.1:1 failed: port is already allocated")
}

func scriptAttempt(errs ...error) (func(int) error, *[]int) {
	var got []int
	i := 0
	return func(port int) error {
		got = append(got, port)
		if i >= len(errs) {
			return fmt.Errorf("attempt extra na porta %d", port)
		}
		e := errs[i]
		i++
		return e
	}, &got
}

func TestStartWithPortRetry_SucessoNaPrimeira(t *testing.T) {
	t.Parallel()
	rec := &recRun{}
	h := &DockerHost{Run: rec.Run, Stderr: io.Discard, Stdout: io.Discard, Stdin: strings.NewReader("")}
	attempt, ports := scriptAttempt(nil)
	got, err := h.StartWithPortRetry(context.Background(), "db-verify-1", 55432, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if got != 55432 {
		t.Fatalf("porta %d, queria 55432", got)
	}
	if len(*ports) != 1 {
		t.Fatalf("attempts %v", *ports)
	}
	if rec.countPrefix("rm") != 0 {
		t.Fatalf("não deveria rm no sucesso: %v", rec.calls)
	}
}

func TestStartWithPortRetry_ConflitoDepoisSucesso(t *testing.T) {
	t.Parallel()
	rec := &recRun{}
	var stderr bytes.Buffer
	h := &DockerHost{Run: rec.Run, Stderr: &stderr, Stdout: io.Discard, Stdin: strings.NewReader("")}
	attempt, ports := scriptAttempt(conflictErr(), nil)
	got, err := h.StartWithPortRetry(context.Background(), "db-verify-1", 3306, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if got != 3307 {
		t.Fatalf("porta %d, queria 3307", got)
	}
	if !rec.has("rm", "-f", "db-verify-1") {
		t.Fatalf("esperava rm -f entre conflitos: %v", rec.calls)
	}
	if !strings.Contains(stderr.String(), "porta 3306 já em uso, tentando 3307") {
		t.Fatalf("aviso stderr: %q", stderr.String())
	}
	if len(*ports) != 2 {
		t.Fatalf("attempts %v", *ports)
	}
}

func TestStartWithPortRetry_ErroNaoConflitoAborta(t *testing.T) {
	t.Parallel()
	rec := &recRun{}
	h := &DockerHost{Run: rec.Run, Stderr: io.Discard, Stdout: io.Discard, Stdin: strings.NewReader("")}
	boom := errors.New("Unable to find image 'nope:99' locally")
	attempt, ports := scriptAttempt(conflictErr(), boom, nil)
	_, err := h.StartWithPortRetry(context.Background(), "c", 1, attempt)
	if err == nil || !strings.Contains(err.Error(), "Unable to find image") {
		t.Fatalf("erro: %v", err)
	}
	if len(*ports) != 2 {
		t.Fatalf("não deveria retentar após não-conflito: %v", *ports)
	}
}

func TestStartWithPortRetry_ErroNaoConflitoRemoveContainer(t *testing.T) {
	t.Parallel()
	rec := &recRun{}
	h := &DockerHost{Run: rec.Run, Stderr: io.Discard, Stdout: io.Discard, Stdin: strings.NewReader("")}
	attempt, _ := scriptAttempt(errors.New("Unable to find image 'nope:99' locally"))
	if _, err := h.StartWithPortRetry(context.Background(), "db-verify-1", 1, attempt); err == nil {
		t.Fatal("queria erro")
	}
	if !rec.has("rm", "-f", "db-verify-1") {
		t.Fatalf("rm -f não registrado: %v", rec.calls)
	}
}

func TestStartWithPortRetry_CtxCanceladoRemoveComCtxVivo(t *testing.T) {
	t.Parallel()
	for _, quando := range []string{"antes", "durante"} {
		t.Run(quando, func(t *testing.T) {
			t.Parallel()
			var rmCtxErr error
			rec := &recRun{fn: func(ctx context.Context, args ...string) ([]byte, error) {
				if args[0] == "rm" {
					rmCtxErr = ctx.Err()
				}
				return nil, nil
			}}
			h := &DockerHost{Run: rec.Run, Stderr: io.Discard, Stdout: io.Discard}
			ctx, cancel := context.WithCancel(context.Background())
			attempt := func(int) error { return nil }
			if quando == "antes" {
				cancel()
			} else {
				attempt = func(int) error { cancel(); return ctx.Err() }
			}
			if _, err := h.StartWithPortRetry(ctx, "c", 1, attempt); !errors.Is(err, context.Canceled) {
				t.Fatalf("erro: %v", err)
			}
			if !rec.has("rm", "-f", "c") {
				t.Fatalf("rm -f não registrado: %v", rec.calls)
			}
			if rmCtxErr != nil {
				t.Fatalf("ctx do rm cancelado: %v", rmCtxErr)
			}
		})
	}
}

func TestStartWithPortRetry_EsgotaSemContainerVaiAoErro(t *testing.T) {
	t.Parallel()
	rec := &recRun{fn: func(ctx context.Context, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "ps" {
			return nil, nil
		}
		return nil, nil
	}}
	h := &DockerHost{Run: rec.Run, Stderr: io.Discard, Stdout: io.Discard, Stdin: strings.NewReader("1\n")}
	attempt, ports := scriptAttempt(conflictErr(), conflictErr(), conflictErr(), conflictErr(), conflictErr())
	_, err := h.StartWithPortRetry(context.Background(), "c", 10, attempt)
	if err == nil || !strings.Contains(err.Error(), "nenhuma porta livre entre 10 e 14") {
		t.Fatalf("erro: %v", err)
	}
	if len(*ports) != 5 {
		t.Fatalf("attempts %v", *ports)
	}
	if rec.countPrefix("ps") == 0 {
		t.Fatal("deveria consultar docker ps após esgotar")
	}
}

func TestStartWithPortRetry_EsgotaPsFalhaPropagaErro(t *testing.T) {
	t.Parallel()
	psErr := errors.New("exit status 1")
	rec := &recRun{fn: func(ctx context.Context, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "ps" {
			return []byte("permission denied while trying to connect to the Docker daemon socket\n"), psErr
		}
		return nil, nil
	}}
	var stdout bytes.Buffer
	h := &DockerHost{Run: rec.Run, Stderr: io.Discard, Stdout: &stdout, Stdin: strings.NewReader("1\n")}
	attempt, _ := scriptAttempt(conflictErr(), conflictErr(), conflictErr(), conflictErr(), conflictErr())
	_, err := h.StartWithPortRetry(context.Background(), "c", 10, attempt)
	if err == nil {
		t.Fatal("queria erro")
	}
	if strings.Contains(err.Error(), "nenhuma porta livre") {
		t.Fatalf("falha do docker ps virou diagnóstico de porta: %v", err)
	}
	if !errors.Is(err, psErr) || !strings.Contains(err.Error(), "listar containers nas portas [10 11 12 13 14]") ||
		!strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("erro: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("não deveria mostrar prompt: %q", stdout.String())
	}
}

func TestStartWithPortRetry_EnterNaoLiberta(t *testing.T) {
	t.Parallel()
	psLine := "abc\tzombie\tpostgres:16\t0.0.0.0:10->5432/tcp\n"
	rec := &recRun{fn: func(ctx context.Context, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "ps" {
			return []byte(psLine), nil
		}
		return nil, nil
	}}
	var stdout bytes.Buffer
	h := &DockerHost{Run: rec.Run, Stderr: io.Discard, Stdout: &stdout, Stdin: strings.NewReader("\n")}
	attempt, _ := scriptAttempt(conflictErr(), conflictErr(), conflictErr(), conflictErr(), conflictErr())
	_, err := h.StartWithPortRetry(context.Background(), "c", 10, attempt)
	if err == nil || !strings.Contains(err.Error(), "nenhuma porta livre") {
		t.Fatalf("erro: %v", err)
	}
	if !strings.Contains(stdout.String(), "número do container para derrubar") {
		t.Fatalf("prompt: %q", stdout.String())
	}
	if rec.has("rm", "-f", "zombie") {
		t.Fatal("Enter não deveria derrubar")
	}
}

func TestStartWithPortRetry_IndiceValidoLibertaERetenta(t *testing.T) {
	t.Parallel()
	psLine := "abc\tzombie\tpostgres:16\t0.0.0.0:10->5432/tcp\n"
	rec := &recRun{fn: func(ctx context.Context, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "ps" {
			return []byte(psLine), nil
		}
		return nil, nil
	}}
	var stdout bytes.Buffer
	h := &DockerHost{Run: rec.Run, Stderr: io.Discard, Stdout: &stdout, Stdin: strings.NewReader("1\n")}
	n := 0
	attempt := func(port int) error {
		n++
		if n <= 5 {
			return conflictErr()
		}
		return nil
	}
	got, err := h.StartWithPortRetry(context.Background(), "c", 10, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if got != 10 {
		t.Fatalf("depois de libertar, faixa recomeça em 10, veio %d", got)
	}
	if !rec.has("rm", "-f", "zombie") {
		t.Fatalf("deveria derrubar escolhido: %v", rec.calls)
	}
	if !strings.Contains(stdout.String(), "derrubando zombie") {
		t.Fatalf("stdout: %q", stdout.String())
	}
	if n != 6 {
		t.Fatalf("5 conflitos + 1 sucesso, veio %d", n)
	}
}

func TestStartWithPortRetry_OpcaoInvalida(t *testing.T) {
	t.Parallel()
	psLine := "abc\tzombie\tpostgres:16\t0.0.0.0:10->5432/tcp\n"
	rec := &recRun{fn: func(ctx context.Context, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "ps" {
			return []byte(psLine), nil
		}
		return nil, nil
	}}
	h := &DockerHost{Run: rec.Run, Stderr: io.Discard, Stdout: io.Discard, Stdin: strings.NewReader("99\n")}
	attempt, _ := scriptAttempt(conflictErr(), conflictErr(), conflictErr(), conflictErr(), conflictErr())
	_, err := h.StartWithPortRetry(context.Background(), "c", 10, attempt)
	if err == nil || !strings.Contains(err.Error(), `opção inválida: "99"`) {
		t.Fatalf("erro: %v", err)
	}
}

func TestStartWithPortRetry_SegundoCicloNaoLoopa(t *testing.T) {
	t.Parallel()
	psLine := "abc\tzombie\tpostgres:16\t0.0.0.0:10->5432/tcp\n"
	psHits := 0
	rec := &recRun{fn: func(ctx context.Context, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "ps" {
			psHits++
			return []byte(psLine), nil
		}
		return nil, nil
	}}
	h := &DockerHost{Run: rec.Run, Stderr: io.Discard, Stdout: io.Discard, Stdin: strings.NewReader("1\n1\n1\n")}
	attempt := func(port int) error { return conflictErr() }
	_, err := h.StartWithPortRetry(context.Background(), "c", 10, attempt)
	if err == nil || !strings.Contains(err.Error(), "nenhuma porta livre entre 10 e 14") {
		t.Fatalf("erro: %v", err)
	}
	if psHits != 5 {
		// um ciclo de prompt: ps por cada porta tentada (5), sem segundo prompt
		t.Fatalf("docker ps hits %d, queria 5 (um ciclo de listagem)", psHits)
	}
}

func TestStartWithPortRetry_CancelaContexto(t *testing.T) {
	t.Parallel()
	h := &DockerHost{Run: func(context.Context, ...string) ([]byte, error) { return nil, nil }, Stderr: io.Discard, Stdout: io.Discard}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.StartWithPortRetry(ctx, "c", 1, func(int) error { return conflictErr() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("erro: %v", err)
	}
}

func TestDockerHost_Available(t *testing.T) {
	t.Parallel()
	h := &DockerHost{
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Run:      func(context.Context, ...string) ([]byte, error) { t.Fatal("não chamar Run"); return nil, nil },
	}
	if err := h.Available(context.Background()); err == nil || !strings.Contains(err.Error(), "não encontrado") {
		t.Fatalf("PATH: %v", err)
	}
	h = &DockerHost{
		LookPath: func(string) (string, error) { return "/usr/bin/docker", nil },
		Run:      func(context.Context, ...string) ([]byte, error) { return nil, errors.New("cannot connect") },
	}
	if err := h.Available(context.Background()); err == nil || !strings.Contains(err.Error(), "não está acessível") {
		t.Fatalf("daemon: %v", err)
	}
	h = &DockerHost{
		LookPath: func(string) (string, error) { return "/usr/bin/docker", nil },
		Run:      func(context.Context, ...string) ([]byte, error) { return []byte("ok"), nil },
	}
	if err := h.Available(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDockerHost_Available_IncluiSaidaEChainErro(t *testing.T) {
	t.Parallel()
	orig := errors.New("exit status 1")
	rec := &recRun{fn: func(context.Context, ...string) ([]byte, error) {
		return []byte("  permission denied on /var/run/docker.sock\n"), orig
	}}
	h := &DockerHost{
		LookPath: func(string) (string, error) { return "/usr/bin/docker", nil },
		Run:      rec.Run,
	}
	err := h.Available(context.Background())
	if err == nil || !strings.Contains(err.Error(), "permission denied on /var/run/docker.sock") {
		t.Fatalf("saída ausente: %v", err)
	}
	if !strings.Contains(err.Error(), "não está acessível") {
		t.Fatalf("prefixo ausente: %v", err)
	}
	if !errors.Is(err, orig) {
		t.Fatalf("erro original não encadeado: %v", err)
	}
	if !strings.Contains(err.Error(), ": permission denied on /var/run/docker.sock: exit status 1") {
		t.Fatalf("saída sem trim: %q", err)
	}
}

func TestDockerHost_Available_PathEncadeiaErro(t *testing.T) {
	t.Parallel()
	orig := errors.New("not found")
	h := &DockerHost{LookPath: func(string) (string, error) { return "", orig }}
	if err := h.Available(context.Background()); !errors.Is(err, orig) {
		t.Fatalf("erro original não encadeado: %v", err)
	}
}

func TestDockerHost_Available_PrazoCurto(t *testing.T) {
	t.Parallel()
	h := &DockerHost{
		LookPath: func(string) (string, error) { return "/usr/bin/docker", nil },
		Run: func(ctx context.Context, _ ...string) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Available(ctx) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("esperava prazo: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Available não retornou no prazo")
	}
}

type fakeListener struct{}

func (fakeListener) Accept() (net.Conn, error) { return nil, io.EOF }
func (fakeListener) Close() error              { return nil }
func (fakeListener) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0} }

func TestDockerHost_FreePortFrom_ListenInjetado(t *testing.T) {
	t.Parallel()
	h := &DockerHost{
		Listen: func(network, address string) (net.Listener, error) {
			if address == "127.0.0.1:3308" {
				return fakeListener{}, nil
			}
			return nil, errors.New("bind")
		},
	}
	if p := h.FreePortFrom(3306); p != 3308 {
		t.Fatalf("porta %d", p)
	}
}

func TestDockerHost_Preflight(t *testing.T) {
	t.Parallel()
	ok := func(string) (string, error) { return "/usr/bin/docker", nil }
	run := func(context.Context, ...string) ([]byte, error) { return nil, nil }
	listen := func(_, address string) (net.Listener, error) {
		if address == "127.0.0.1:5001" {
			return fakeListener{}, nil
		}
		return nil, errors.New("bind")
	}
	h := &DockerHost{LookPath: ok, Run: run, Listen: listen}
	if p, err := h.Preflight(context.Background(), 0, 5000); err != nil || p != 5001 {
		t.Fatalf("porta omitida: %d, %v", p, err)
	}
	if p, err := h.Preflight(context.Background(), 7000, 5000); err != nil || p != 7000 {
		t.Fatalf("--port explícito: %d, %v", p, err)
	}
	down := &DockerHost{
		LookPath: ok,
		Run:      func(context.Context, ...string) ([]byte, error) { return nil, errors.New("cannot connect") },
		Listen: func(string, string) (net.Listener, error) {
			t.Fatal("não procurar porta com daemon fora")
			return nil, nil
		},
	}
	if _, err := down.Preflight(context.Background(), 0, 5000); err == nil {
		t.Fatal("esperava erro com daemon fora")
	}
}

type exitErr int

func (e exitErr) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitErr) ExitCode() int { return int(e) }

func TestExitCode(t *testing.T) {
	t.Parallel()
	if code, ok := ExitCode(fmt.Errorf("restore: %w", exitErr(3))); !ok || code != 3 {
		t.Fatalf("encadeado: %d, %v", code, ok)
	}
	if _, ok := ExitCode(errors.New("executable file not found")); ok {
		t.Fatal("erro sem exit code reconhecido como saída do processo")
	}
	if _, ok := ExitCode(nil); ok {
		t.Fatal("nil reconhecido como saída do processo")
	}
}

func TestDockerHost_DockerInput_RepassaStdin(t *testing.T) {
	t.Parallel()
	var got string
	var gotArgs []string
	h := &DockerHost{RunInput: func(_ context.Context, r io.Reader, args ...string) ([]byte, error) {
		b, err := io.ReadAll(r)
		got, gotArgs = string(b), args
		return []byte("out"), err
	}}
	out, err := h.DockerInput(context.Background(), strings.NewReader("dump"), "exec", "-i", "c", "cat")
	if err != nil || string(out) != "out" || got != "dump" || strings.Join(gotArgs, " ") != "exec -i c cat" {
		t.Fatalf("out=%q err=%v stdin=%q args=%v", out, err, got, gotArgs)
	}
}

func TestDockerHost_RemoveELogs(t *testing.T) {
	t.Parallel()
	rec := &recRun{fn: func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] == "logs" {
			return []byte("log"), nil
		}
		return nil, errors.New("No such container")
	}}
	h := &DockerHost{Run: rec.Run}
	h.Remove("c")
	if !rec.has("rm", "-f", "c") {
		t.Fatalf("rm -f ausente: %v", rec.calls)
	}
	if got := string(h.Logs("c", 20)); got != "log" || !rec.has("logs", "--tail", "20", "c") {
		t.Fatalf("logs com tail: %q %v", got, rec.calls)
	}
	h.Logs("c", 0)
	if !rec.has("logs", "c") {
		t.Fatalf("logs sem tail: %v", rec.calls)
	}
}

func TestDockerHost_WaitReady_ExigeDuasSondasSeguidas(t *testing.T) {
	t.Parallel()
	// falha, sucesso, falha (reinício do servidor temporário), sucesso, sucesso.
	script := []error{errors.New("down"), nil, errors.New("restarting"), nil, nil}
	var mu sync.Mutex
	rec := &recRun{fn: func(context.Context, ...string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		err := script[0]
		script = script[1:]
		return nil, err
	}}
	h := &DockerHost{Run: rec.Run}
	if err := h.WaitReady(context.Background(), "c", "Postgres", 30*time.Second, "pg_isready", "-U", "postgres"); err != nil {
		t.Fatal(err)
	}
	if n := rec.countPrefix("exec"); n != 5 {
		t.Fatalf("sondas = %d, queria 5", n)
	}
	if !rec.has("exec", "c", "pg_isready", "-U", "postgres") {
		t.Fatalf("sonda errada: %v", rec.calls)
	}
}

func TestDockerHost_WaitReady_TimeoutTrazLogs(t *testing.T) {
	t.Parallel()
	rec := &recRun{fn: func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] == "logs" {
			return []byte("FATAL: boom"), nil
		}
		return nil, errors.New("down")
	}}
	h := &DockerHost{Run: rec.Run}
	err := h.WaitReady(context.Background(), "c", "MySQL", 50*time.Millisecond, "mysql", "-e", "SELECT 1")
	if err == nil || !strings.Contains(err.Error(), "timeout esperando o MySQL") || !strings.Contains(err.Error(), "FATAL: boom") {
		t.Fatalf("erro: %v", err)
	}
	if !rec.has("logs", "--tail", "20", "c") {
		t.Fatalf("logs ausentes: %v", rec.calls)
	}
}

func TestDockerHost_WaitReady_CtxCancelado(t *testing.T) {
	t.Parallel()
	h := &DockerHost{Run: func(context.Context, ...string) ([]byte, error) { return nil, errors.New("down") }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.WaitReady(ctx, "c", "Postgres", time.Minute, "true"); !errors.Is(err, context.Canceled) {
		t.Fatalf("erro: %v", err)
	}
}
