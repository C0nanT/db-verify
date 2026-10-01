// Pacote docker concentra o host Docker: scan de porta, parse de conflito,
// retry, rm do nome fixo, docker ps na faixa, prompt ao operador, “daemon
// responde?” e o ciclo de vida do container (run/cp/exec/rm/logs, espera de
// prontidão). Engines Docker recebem um *DockerHost e não chamam a CLI
// Docker por conta própria; a policy Engine/Session não conhece CLI Docker
// nem stdin. Produção usa exec + stdin/stderr reais; testes substituem Run,
// RunInput, o leitor do prompt e Listen.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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

// maxPortRetries é quantas portas seguidas StartWithPortRetry tenta antes de
// oferecer ao operador derrubar um container. FreePortFrom já
// evitam a maioria dos conflitos no host, mas resta a corrida entre o
// listen e o docker publicar — e --port pula o scan.
const maxPortRetries = 5

// removeTimeout é o prazo do rm -f de limpeza, independente do ctx do chamador.
const removeTimeout = 10 * time.Second

// dockerRun executa `docker args...` e devolve stdout+stderr juntos.
type dockerRun func(ctx context.Context, args ...string) ([]byte, error)

// dockerRunInput é dockerRun com stdin vindo de um io.Reader (stream de dump
// para `docker exec -i`/`docker cp -`).
type dockerRunInput func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error)

// dockerListen tenta abrir um TCP listen (scan de porta livre).
type dockerListen func(network, address string) (net.Listener, error)

// DockerHost é o caminho único das engines para a CLI Docker: porta,
// daemon, ciclo de vida do container (run/cp/exec/rm/logs) e espera de
// prontidão. Campos nil — e um *DockerHost nil — caem no comportamento de
// produção (LookPath/exec/Listen/os.Stdin/Stderr/Stdout).
type DockerHost struct {
	LookPath func(file string) (string, error)
	Run      dockerRun
	RunInput dockerRunInput
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

func (h *DockerHost) runInput() dockerRunInput {
	if h != nil && h.RunInput != nil {
		return h.RunInput
	}
	return func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Stdin = stdin
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		return out.Bytes(), err
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

// dockerCheckTimeout limita a checagem do Docker: daemon travado não pode
// bloquear o Provision para sempre.
const dockerCheckTimeout = 10 * time.Second

// ContainerName é o nome fixo do container de verificação deste processo.
func ContainerName() string {
	return fmt.Sprintf("db-verify-%d", os.Getpid())
}

// ExitCode extrai o exit code de um erro devolvido por Docker/DockerInput
// quando o processo rodou e saiu com código != 0 (em produção,
// *exec.ExitError). ok=false: o erro é de outra natureza (binário ausente,
// ctx cancelado antes de subir…).
func ExitCode(err error) (code int, ok bool) {
	var ee interface{ ExitCode() int }
	if errors.As(err, &ee) {
		return ee.ExitCode(), true
	}
	return 0, false
}

// Preflight é o prólogo comum do Provision: confere o daemon (com
// dockerCheckTimeout sobre o ctx do Provision) e escolhe a porta inicial —
// port quando veio de --port, senão a primeira livre a partir de defaultPort.
func (h *DockerHost) Preflight(ctx context.Context, port, defaultPort int) (int, error) {
	checkCtx, cancel := context.WithTimeout(ctx, dockerCheckTimeout)
	defer cancel()
	if err := h.Available(checkCtx); err != nil {
		return 0, err
	}
	if port == 0 {
		port = h.FreePortFrom(defaultPort)
	}
	return port, nil
}

// Docker executa `docker args...` e devolve stdout+stderr juntos.
func (h *DockerHost) Docker(ctx context.Context, args ...string) ([]byte, error) {
	return h.run()(ctx, args...)
}

// DockerInput é Docker com stdin lido de r. Um erro de leitura de r (dump
// corrompido no meio da descompressão) volta como erro.
func (h *DockerHost) DockerInput(ctx context.Context, r io.Reader, args ...string) ([]byte, error) {
	return h.runInput()(ctx, r, args...)
}

// Remove roda `rm -f name`, ignorando erro: é idempotente, o container pode
// nem existir. Sem ctx de propósito: é chamado por Session.Close e por
// caminhos de falha em que o ctx do chamador já pode estar cancelado.
func (h *DockerHost) Remove(name string) {
	_, _ = h.run()(context.Background(), "rm", "-f", name)
}

// Logs devolve stdout+stderr do container; tail > 0 limita às últimas
// linhas. Sem ctx, como Remove: serve para diagnosticar falhas depois que o
// ctx do chamador já pode ter acabado.
func (h *DockerHost) Logs(name string, tail int) []byte {
	args := []string{"logs"}
	if tail > 0 {
		args = append(args, "--tail", strconv.Itoa(tail))
	}
	out, _ := h.run()(context.Background(), append(args, name)...)
	return out
}

// WaitReady roda `docker exec name probe...` até a sonda ter sucesso duas
// vezes seguidas, com um respiro entre elas. As imagens oficiais de Postgres
// e MySQL/MariaDB sobem um "servidor temporário" para rodar a inicialização
// e só depois reiniciam para o servidor real: uma sonda bem-sucedida contra o
// temporário dá falso positivo, e testar bem na fronteira do reinício pode
// achar o socket fechado logo depois de uma sonda ter funcionado — daí a
// segunda checagem. Esgotado o timeout, o erro traz as últimas 20 linhas do
// log do container; what nomeia o servidor na mensagem.
func (h *DockerHost) WaitReady(ctx context.Context, name, what string, timeout time.Duration, probe ...string) error {
	args := append([]string{"exec", name}, probe...)
	check := func() bool {
		_, err := h.run()(ctx, args...)
		return err == nil
	}
	sleep := func(d time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
			return nil
		}
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			if err := sleep(300 * time.Millisecond); err != nil {
				return err
			}
			if check() {
				return nil
			}
			continue
		}
		if err := sleep(min(time.Second, time.Until(deadline))); err != nil {
			return err
		}
	}
	return fmt.Errorf("timeout esperando o %s:\n%s", what, h.Logs(name, 20))
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
// maxPortRetries. Outro erro (ou ctx cancelado) aborta, também com rm -f do
// nome: quem chama não precisa limpar o container de uma subida que falhou.
// Esgotou: prompt; se libertou, um segundo ciclo de cinco tentativas;
// segundo esgotamento é erro (sem loop).
func (h *DockerHost) StartWithPortRetry(ctx context.Context, name string, startPort int, attempt func(port int) error) (int, error) {
	return h.startWithPortRetry(ctx, name, startPort, attempt, false)
}

// removeContainer roda `rm -f name` com contexto próprio, sem herdar o
// cancelamento do chamador (com o ctx já cancelado o docker rm nem
// executaria) e com prazo curto. É idempotente: o container pode nem existir.
func (h *DockerHost) removeContainer(ctx context.Context, name string) {
	rmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), removeTimeout)
	defer cancel()
	_, _ = h.run()(rmCtx, "rm", "-f", name)
}

func (h *DockerHost) startWithPortRetry(ctx context.Context, name string, startPort int, attempt func(port int) error, extraCycle bool) (int, error) {
	port := startPort
	var lastErr error
	tried := make([]int, 0, maxPortRetries)
	for i := 0; i < maxPortRetries; i++ {
		if err := ctx.Err(); err != nil {
			h.removeContainer(ctx, name)
			return 0, err
		}
		tried = append(tried, port)
		err := attempt(port)
		if err == nil {
			return port, nil
		}
		h.removeContainer(ctx, name)
		if !isPortConflict(err.Error()) {
			return 0, err
		}
		lastErr = err
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
			if msg := strings.TrimSpace(string(out)); msg != "" {
				return nil, fmt.Errorf("%s: %w", msg, err)
			}
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
	if err != nil {
		// Falha do `docker ps` não é "nenhum container na faixa": reportá-la
		// como falta de porta livre daria ao operador o diagnóstico errado.
		return false, fmt.Errorf("listar containers nas portas %v: %w", tried, err)
	}
	if len(containers) == 0 {
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
