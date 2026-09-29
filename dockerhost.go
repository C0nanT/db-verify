package main

// Módulo de host Docker: scan de porta, parse de conflito, retry, rm do
// nome fixo, docker ps na faixa, prompt ao operador e “daemon responde?”.
// Engines Docker chamam daqui; a policy Engine/Session não conhece CLI
// Docker nem stdin. Produção usa exec + stdin/stderr reais; testes
// substituem Run, o leitor do prompt e Listen.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// postgresPortWindow é o início da janela Postgres quando --port não veio.
const postgresPortWindow = 55432

// maxPortRetries é quantas portas seguidas StartWithPortRetry tenta antes de
// oferecer ao operador derrubar um container. freePort/freePortFrom já
// evitam a maioria dos conflitos no host, mas resta a corrida entre o
// listen e o docker publicar — e --port pula o scan.
const maxPortRetries = 5

// dockerRun executa `docker args...` e devolve stdout+stderr juntos.
type dockerRun func(ctx context.Context, args ...string) ([]byte, error)

// dockerListen tenta abrir um TCP listen (scan de porta livre).
type dockerListen func(network, address string) (net.Listener, error)

// DockerHost concentra I/O de porta + CLI Docker. Campos nil caem no
// comportamento de produção (LookPath/exec/Listen/os.Stdin/Stderr/Stdout).
type DockerHost struct {
	LookPath func(file string) (string, error)
	Run      dockerRun
	Listen   dockerListen
	Stdin    io.Reader
	Stderr   io.Writer
	Stdout   io.Writer
}

func (h *DockerHost) lookPath() func(string) (string, error) {
	if h != nil && h.LookPath != nil {
		return h.LookPath
	}
	return exec.LookPath
}

func (h *DockerHost) run() dockerRun {
	if h != nil && h.Run != nil {
		return h.Run
	}
	return func(ctx context.Context, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	}
}

func (h *DockerHost) listen() dockerListen {
	if h != nil && h.Listen != nil {
		return h.Listen
	}
	return net.Listen
}

func (h *DockerHost) stdin() io.Reader {
	if h != nil && h.Stdin != nil {
		return h.Stdin
	}
	return os.Stdin
}

func (h *DockerHost) stderr() io.Writer {
	if h != nil && h.Stderr != nil {
		return h.Stderr
	}
	return os.Stderr
}

func (h *DockerHost) stdout() io.Writer {
	if h != nil && h.Stdout != nil {
		return h.Stdout
	}
	return os.Stdout
}

// defaultDockerHost é o host de produção usado pelas engines.
var defaultDockerHost = &DockerHost{}

// dockerCheckTimeout limita a checagem do Docker: daemon travado não pode
// bloquear o Provision para sempre.
const dockerCheckTimeout = 10 * time.Second

// dockerAvailable aplica dockerCheckTimeout sobre o ctx do Provision.
func dockerAvailable(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, dockerCheckTimeout)
	defer cancel()
	return defaultDockerHost.Available(ctx)
}

// freePort procura uma porta livre a partir de 55432 (janela Postgres).
func freePort() int {
	return defaultDockerHost.FreePortFrom(postgresPortWindow)
}

// freePortFrom procura a primeira porta livre a partir de start (inclusive),
// numa janela de 100 portas — porta padrão do engine quando estiver livre.
func freePortFrom(start int) int {
	return defaultDockerHost.FreePortFrom(start)
}

func startWithPortRetry(ctx context.Context, name string, startPort int, attempt func(port int) error) (int, error) {
	return defaultDockerHost.StartWithPortRetry(ctx, name, startPort, attempt)
}

// Available verifica se o binário docker está no PATH e o daemon responde.
// Os erros encadeiam a causa original e, quando o daemon falha, trazem a
// saída do `docker info` (permissão no socket, daemon parado, DOCKER_HOST).
func (h *DockerHost) Available(ctx context.Context) error {
	if _, err := h.lookPath()("docker"); err != nil {
		return fmt.Errorf("docker não encontrado no PATH: %w", err)
	}
	if out, err := h.run()(ctx, "info"); err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("docker daemon não está acessível: %s: %w", msg, err)
		}
		return fmt.Errorf("docker daemon não está acessível: %w", err)
	}
	return nil
}

// FreePortFrom devolve a primeira porta TCP livre em [start, start+100).
// Se nenhuma aceitar listen, devolve start (o retry cobre o conflito).
func (h *DockerHost) FreePortFrom(start int) int {
	listen := h.listen()
	for p := start; p < start+100; p++ {
		l, err := listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			l.Close()
			return p
		}
	}
	return start
}

// isPortConflict reconhece, na mensagem do docker, bind/allocate em uso.
func isPortConflict(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, "port is already allocated") ||
		strings.Contains(low, "address already in use") ||
		(strings.Contains(low, "bind for") && strings.Contains(low, "failed"))
}

// StartWithPortRetry chama attempt(port) a partir de startPort. Conflito
// de porta: rm -f do nome fixo, aviso em stderr, próxima porta, até
// maxPortRetries. Outro erro aborta. Esgotou: prompt; se libertou, um
// segundo ciclo de cinco tentativas; segundo esgotamento é erro (sem loop).
func (h *DockerHost) StartWithPortRetry(ctx context.Context, name string, startPort int, attempt func(port int) error) (int, error) {
	return h.startWithPortRetry(ctx, name, startPort, attempt, false)
}

func (h *DockerHost) startWithPortRetry(ctx context.Context, name string, startPort int, attempt func(port int) error, extraCycle bool) (int, error) {
	port := startPort
	var lastErr error
	tried := make([]int, 0, maxPortRetries)
	for i := 0; i < maxPortRetries; i++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		tried = append(tried, port)
		err := attempt(port)
		if err == nil {
			return port, nil
		}
		if !isPortConflict(err.Error()) {
			return 0, err
		}
		lastErr = err
		_, _ = h.run()(ctx, "rm", "-f", name)
		fmt.Fprintf(h.stderr(), "! porta %d já em uso, tentando %d…\n", port, port+1)
		port++
	}

	if extraCycle {
		return 0, fmt.Errorf("nenhuma porta livre entre %d e %d: %w", tried[0], tried[len(tried)-1], lastErr)
	}

	freed, err := h.offerToFreePort(ctx, tried)
	if err != nil {
		return 0, err
	}
	if !freed {
		return 0, fmt.Errorf("nenhuma porta livre entre %d e %d: %w", tried[0], tried[len(tried)-1], lastErr)
	}
	return h.startWithPortRetry(ctx, name, startPort, attempt, true)
}

type portUser struct {
	Name  string
	Image string
	Ports string
}

func (h *DockerHost) containersUsingPorts(ctx context.Context, ports []int) ([]portUser, error) {
	seen := map[string]portUser{}
	for _, p := range ports {
		out, err := h.run()(ctx, "ps",
			"--filter", fmt.Sprintf("publish=%d", p),
			"--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Ports}}")
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			f := strings.SplitN(line, "\t", 4)
			if len(f) != 4 {
				continue
			}
			seen[f[0]] = portUser{Name: f[1], Image: f[2], Ports: f[3]}
		}
	}
	out := make([]portUser, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (h *DockerHost) offerToFreePort(ctx context.Context, tried []int) (bool, error) {
	containers, err := h.containersUsingPorts(ctx, tried)
	if err != nil || len(containers) == 0 {
		return false, nil
	}

	fmt.Fprintln(h.stdout())
	fmt.Fprintf(h.stdout(), "! %d tentativas de porta (%d–%d) falharam; containers docker publicando portas nessa faixa:\n",
		len(tried), tried[0], tried[len(tried)-1])
	for i, c := range containers {
		fmt.Fprintf(h.stdout(), "  [%d] %-30s %-25s %s\n", i+1, c.Name, c.Image, c.Ports)
	}
	fmt.Fprint(h.stdout(), "número do container para derrubar (enter cancela): ")

	line, _ := bufio.NewReader(h.stdin()).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return false, nil
	}
	idx, convErr := strconv.Atoi(line)
	if convErr != nil || idx < 1 || idx > len(containers) {
		return false, fmt.Errorf("opção inválida: %q", line)
	}
	chosen := containers[idx-1]
	fmt.Fprintf(h.stdout(), "derrubando %s…\n", chosen.Name)
	if out, err := h.run()(ctx, "rm", "-f", chosen.Name); err != nil {
		return false, fmt.Errorf("falha ao derrubar %s: %s", chosen.Name, strings.TrimSpace(string(out)))
	}
	return true, nil
}
