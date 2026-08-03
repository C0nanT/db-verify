// db-verify sobe um Postgres em Docker, restaura um .dump e abre uma TUI
// para inspecionar as tabelas e os 20 registros mais recentes de cada uma.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	var (
		pgVersion = flag.String("pg", "", "versão do Postgres (padrão: a mesma do dump)")
		port      = flag.Int("port", 0, "porta no host (padrão: primeira livre a partir de 55432)")
		jobs      = flag.Int("jobs", 4, "paralelismo do pg_restore")
		dbName    = flag.String("db", "verify", "nome do banco de destino")
		keep      = flag.Bool("keep", false, "não remover o container ao sair")
		noCounts  = flag.Bool("no-counts", false, "usar contagem estimada em vez de count(*)")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "uso: db-verify [flags] [arquivo.dump]\n\nsem argumento, lista os .dump encontrados em ./data\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	var dumpPath string
	switch flag.NArg() {
	case 0:
		dataDir, err := defaultDataDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n%s %v\n", stErr.Render("✗"), err)
			os.Exit(1)
		}
		dumpPath, err = pickDump(dataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n%s %v\n", stErr.Render("✗"), err)
			os.Exit(1)
		}
	case 1:
		dumpPath = flag.Arg(0)
	default:
		flag.Usage()
		os.Exit(2)
	}
	if err := run(dumpPath, *pgVersion, *port, *jobs, *dbName, *keep, !*noCounts); err != nil {
		fmt.Fprintf(os.Stderr, "\n%s %v\n", stErr.Render("✗"), err)
		os.Exit(1)
	}
}

func step(format string, a ...any) {
	fmt.Printf("%s %s\n", stAccent.Render("==>"), fmt.Sprintf(format, a...))
}

func run(path, pgVersion string, port, jobs int, dbName string, keep, exactCounts bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := dockerAvailable(); err != nil {
		return err
	}

	info, err := InspectDump(abs)
	if err != nil {
		return err
	}
	if pgVersion == "" {
		pgVersion = info.PGMajor
		if pgVersion == "" {
			pgVersion = "16"
		}
	}
	if port == 0 {
		port = freePort()
	}

	cont := &Container{
		Name:  fmt.Sprintf("db-verify-%d", os.Getpid()),
		Image: "postgres:" + pgVersion + "-alpine",
		Port:  port, DB: dbName, User: "postgres", Pass: "postgres",
	}

	fmt.Println()
	fmt.Println(stTitle.Render("Verify Backup"))
	fmt.Printf("  %s %s (%s)\n", stLabel.Render("arquivo    :"), abs, humanSize(info.Size))
	fmt.Printf("  %s %s / compressão %s\n", stLabel.Render("formato    :"), info.Format, info.Compression)
	if info.OriginDB != "" {
		fmt.Printf("  %s %s\n", stLabel.Render("banco orig.:"), info.OriginDB)
	}
	fmt.Printf("  %s %s → %s\n", stLabel.Render("versão     :"), orDash(info.PGMajor), cont.Image)
	fmt.Println()

	// derruba o container em Ctrl+C também
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		if !keep {
			cont.Remove()
		}
		os.Exit(130)
	}()
	defer func() {
		if keep {
			fmt.Printf("\n%s container mantido: %s\n", stAccent.Render("==>"), cont.Name)
			fmt.Printf("    psql:   docker exec -it %s psql -U %s -d %s\n", cont.Name, cont.User, cont.DB)
			fmt.Printf("    remove: docker rm -f %s\n", cont.Name)
		} else {
			cont.Remove()
		}
	}()

	step("subindo container %s…", cont.Name)
	if err := cont.Start(ctx); err != nil {
		return err
	}
	step("aguardando o Postgres ficar pronto…")
	if err := cont.WaitReady(ctx, 90*time.Second); err != nil {
		return err
	}
	step("copiando dump para o container…")
	if err := cont.CopyDump(ctx, info); err != nil {
		return err
	}
	step("restaurando (pode demorar)…")
	res, err := cont.Restore(ctx, info, jobs)
	if err != nil {
		return err
	}
	if len(res.Errors) == 0 {
		fmt.Printf("%s restore concluído sem erros em %s\n", stOK.Render("✓"), res.Duration.Round(time.Millisecond))
	} else {
		fmt.Printf("%s restore com %d erro(s) em %s\n", stWarn.Render("!"), len(res.Errors), res.Duration.Round(time.Millisecond))
		for i, e := range res.Errors {
			if i == 5 {
				fmt.Printf("    %s\n", stDim.Render(fmt.Sprintf("… mais %d", len(res.Errors)-5)))
				break
			}
			fmt.Printf("    %s\n", truncate(e, 110))
		}
		if res.LogPath != "" {
			fmt.Printf("    %s\n", stDim.Render("log: "+res.LogPath))
		}
	}

	step("consultando o banco…")
	pool, err := Connect(ctx, cont.DSN())
	if err != nil {
		return fmt.Errorf("conexão falhou: %w", err)
	}
	defer pool.Close()

	health, err := FetchHealth(ctx, pool)
	if err != nil {
		return err
	}
	tables, err := FetchTables(ctx, pool, exactCounts)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return fmt.Errorf("o backup não gerou nenhuma tabela — provavelmente está corrompido ou vazio")
	}

	p := tea.NewProgram(
		newModel(pool, info, cont, res, health, tables),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		return err
	}

	fmt.Printf("\n%s conexão para reusar:\n  psql \"%s\"\n", stAccent.Render("==>"), cont.DSN())
	return nil
}

// defaultDataDir resolve a pasta "data" ao lado do binário (raiz do projeto).
func defaultDataDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "data"), nil
}

func orDash(s string) string {
	if s == "" {
		return "desconhecida"
	}
	return s
}
