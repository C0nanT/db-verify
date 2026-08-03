// db-verify sobe o banco certo em Docker, restaura um backup e abre uma TUI
// para inspecionar as coleções e os 20 registros mais recentes de cada uma.
// A escolha de engine (Postgres, e no futuro outras) fica inteiramente
// atrás da interface Engine/Session — este arquivo só conhece o registro.
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
		versionTag  = flag.String("version-tag", "", "versão da imagem da engine (padrão: a mesma do backup)")
		pgVersion   = flag.String("pg", "", "alias depreciado de --version-tag")
		port        = flag.Int("port", 0, "porta no host (padrão: primeira livre a partir de 55432)")
		jobs        = flag.Int("jobs", 4, "paralelismo do restore, quando a engine suportar")
		dbName      = flag.String("db", "verify", "nome do banco de destino")
		keep        = flag.Bool("keep", false, "não remover o container ao sair")
		noCounts    = flag.Bool("no-counts", false, "usar contagem estimada em vez de count(*)")
		engineName  = flag.String("engine", "", "força a engine (pula a detecção); veja --list-engines")
		listEngines = flag.Bool("list-engines", false, "lista as engines suportadas e sai")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "uso: db-verify [flags] [arquivo]\n\nsem argumento, lista os backups encontrados em ./data\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *listEngines {
		for _, e := range Engines() {
			fmt.Printf("%s\n    %s\n", e.Name(), e.Expects())
		}
		return
	}

	if *pgVersion != "" {
		fmt.Fprintf(os.Stderr, "%s --pg está depreciado, use --version-tag\n", stWarn.Render("!"))
		if *versionTag == "" {
			*versionTag = *pgVersion
		}
	}

	if *engineName != "" {
		if _, ok := Lookup(*engineName); !ok {
			fmt.Fprintf(os.Stderr, "\n%s %v\n", stErr.Render("✗"), unknownEngineErr(*engineName))
			os.Exit(1)
		}
	}

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
	if err := run(dumpPath, *versionTag, *engineName, *port, *jobs, *dbName, *keep, !*noCounts); err != nil {
		fmt.Fprintf(os.Stderr, "\n%s %v\n", stErr.Render("✗"), err)
		os.Exit(1)
	}
}

func step(format string, a ...any) {
	fmt.Printf("%s %s\n", stAccent.Render("==>"), fmt.Sprintf(format, a...))
}

func run(path, versionTag, engineName string, port, jobs int, dbName string, keep, exactCounts bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	backup, err := InspectDumpAs(abs, engineName)
	if err != nil {
		return err
	}
	if backup.Guessed {
		fmt.Fprintf(os.Stderr, "%s não foi possível identificar o formato do arquivo com confiança; presumindo %s. Use --engine para forçar outra.\n", stWarn.Render("!"), backup.Engine)
	}
	eng, ok := Lookup(backup.Engine)
	if !ok {
		return fmt.Errorf("engine %q não registrada", backup.Engine)
	}

	fmt.Println()
	fmt.Println(stTitle.Render("Verify Backup"))
	fmt.Printf("  %s %s (%s)\n", stLabel.Render("arquivo    :"), abs, humanSize(backup.Size))
	fmt.Printf("  %s %s / compressão %s\n", stLabel.Render("formato    :"), backup.Format, backup.Compression)
	if backup.OriginDB != "" {
		fmt.Printf("  %s %s\n", stLabel.Render("banco orig.:"), backup.OriginDB)
	}
	fmt.Printf("  %s %s (%s)\n", stLabel.Render("versão     :"), orDash(backup.Version), eng.Name())
	fmt.Println()

	opts := ProvisionOpts{
		VersionTag: versionTag, Port: port, Jobs: jobs, DBName: dbName,
		ExactCounts: exactCounts, Progress: step,
	}
	sess, err := eng.Provision(ctx, backup, opts)
	if err != nil {
		return err
	}

	// derruba a sessão em Ctrl+C também
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		if !keep {
			sess.Close()
		}
		os.Exit(130)
	}()
	defer func() {
		if keep {
			hint := sess.ConnectHint()
			// hint.Name fica vazio para engines sem container (SQLite,
			// hoje) — nesse caso, o que --keep preserva é o próprio
			// caminho do arquivo temporário, carregado em DSN.
			label := hint.Name
			if label == "" {
				label = hint.DSN
			}
			fmt.Printf("\n%s sessão mantida: %s\n", stAccent.Render("==>"), label)
			if hint.ExecShell != "" {
				fmt.Printf("    shell:  %s\n", hint.ExecShell)
			}
			if hint.Remove != "" {
				fmt.Printf("    remove: %s\n", hint.Remove)
			}
		} else {
			sess.Close()
		}
	}()

	if res := sess.Restore(); res != nil {
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
	}

	step("consultando o banco…")
	health, err := sess.Health(ctx)
	if err != nil {
		return err
	}
	collections, err := sess.Collections(ctx, exactCounts)
	if err != nil {
		return err
	}
	if len(collections) == 0 {
		return fmt.Errorf("o backup não gerou nenhuma coleção — provavelmente está corrompido ou vazio")
	}

	p := tea.NewProgram(
		newModel(sess, backup, health, collections),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		return err
	}

	hint := sess.ConnectHint()
	fmt.Printf("\n%s conexão para reusar:\n  %s\n", stAccent.Render("==>"), hint.Shell)
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
