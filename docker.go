package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Container representa o Postgres temporário usado para validar o backup.
type Container struct {
	Name  string
	Image string
	Port  int
	DB    string
	User  string
	Pass  string
}

func (c *Container) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@127.0.0.1:%d/%s?sslmode=disable",
		c.User, c.Pass, c.Port, c.DB)
}

func dockerAvailable() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker não encontrado no PATH")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		return fmt.Errorf("docker daemon não está acessível")
	}
	return nil
}

// freePort procura uma porta livre a partir de 55432.
func freePort() int {
	for p := 55432; p < 55532; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			l.Close()
			return p
		}
	}
	return 55432
}

// Start sobe o container com fsync desligado (restore mais rápido, dado descartável).
func (c *Container) Start(ctx context.Context) error {
	args := []string{
		"run", "-d", "--name", c.Name,
		"-e", "POSTGRES_PASSWORD=" + c.Pass,
		"-e", "POSTGRES_USER=" + c.User,
		"-e", "POSTGRES_DB=" + c.DB,
		"-p", fmt.Sprintf("127.0.0.1:%d:5432", c.Port),
		c.Image,
		"-c", "fsync=off", "-c", "full_page_writes=off", "-c", "synchronous_commit=off",
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("falha ao subir container: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *Container) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := exec.CommandContext(ctx, "docker", "exec", c.Name,
			"pg_isready", "-U", c.User, "-d", c.DB).Run()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	logs, _ := exec.Command("docker", "logs", "--tail", "20", c.Name).CombinedOutput()
	return fmt.Errorf("timeout esperando o Postgres:\n%s", string(logs))
}

// CopyDump joga o arquivo dentro do container, descomprimindo se preciso.
func (c *Container) CopyDump(ctx context.Context, info *DumpInfo) error {
	if info.Compression == "none" {
		out, err := exec.CommandContext(ctx, "docker", "cp", info.Path, c.Name+":/tmp/backup.dump").CombinedOutput()
		if err != nil {
			return fmt.Errorf("docker cp falhou: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}
	r, _, err := openMaybeCompressed(info.Path)
	if err != nil {
		return err
	}
	defer r.Close()

	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", c.Name, "sh", "-c", "cat > /tmp/backup.dump")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	_, copyErr := io.Copy(stdin, r)
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("falha ao copiar dump: %w", err)
	}
	return copyErr
}

// RestoreResult resume o que aconteceu no pg_restore/psql.
type RestoreResult struct {
	Errors   []string
	Duration time.Duration
	LogPath  string
	ExitCode int
}

var reRestoreErr = regexp.MustCompile(`(?im)^(pg_restore: )?(error|erro):|^ERROR:`)

func (c *Container) Restore(ctx context.Context, info *DumpInfo, jobs int) (*RestoreResult, error) {
	start := time.Now()
	var args []string
	if info.Format == "plain" {
		args = []string{"exec", c.Name, "psql", "-U", c.User, "-d", c.DB,
			"-v", "ON_ERROR_STOP=0", "-f", "/tmp/backup.dump"}
	} else {
		args = []string{"exec", c.Name, "pg_restore", "-U", c.User, "-d", c.DB,
			"--no-owner", "--no-privileges", fmt.Sprintf("--jobs=%d", jobs), "/tmp/backup.dump"}
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()

	res := &RestoreResult{Duration: time.Since(start)}
	if ee, ok := err.(*exec.ExitError); ok {
		res.ExitCode = ee.ExitCode()
	} else if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if reRestoreErr.MatchString(line) {
			res.Errors = append(res.Errors, strings.TrimSpace(line))
		}
	}
	if len(res.Errors) > 0 {
		if f, e := os.CreateTemp("", "db-verify-*.log"); e == nil {
			f.Write(out)
			f.Close()
			res.LogPath = f.Name()
		}
	}
	return res, nil
}

func (c *Container) Remove() {
	exec.Command("docker", "rm", "-f", c.Name).Run()
}
