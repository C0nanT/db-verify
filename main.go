// db-verify sobe o banco certo em Docker, restaura um backup e abre uma TUI
// para inspecionar as coleções e os 20 registros mais recentes de cada uma.
// A escolha de engine (Postgres, e no futuro outras) fica inteiramente
// atrás da interface Engine/Session — este arquivo só conhece o registro.
package main

import (
	"context"
	"db-verify/internal/detect"
	"db-verify/internal/engine"
	"db-verify/internal/ui"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// portFlagUsage descreve --port: valor explícito é a primeira tentativa;
// omitido, a janela da engine (constante <engine>DefaultPort do seu pacote).
const portFlagUsage = "porta no host (explícito: primeira tentativa; omitido: janela da engine)"

func main() {
	var (
		versionTag  = flag.String("version-tag", "", "versão da imagem da engine (padrão: a mesma do backup)")
		pgVersion   = flag.String("pg", "", "alias depreciado de --version-tag")
		port        = flag.Int("port", 0, portFlagUsage)
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
		for _, e := range engine.Engines() {
			fmt.Printf("%s\n    %s\n", e.Name(), e.Expects())
		}
		return
	}

	if *pgVersion != "" {
		fmt.Fprintf(os.Stderr, "%s --pg está depreciado, use --version-tag\n", ui.StWarn.Render("!"))
		if *versionTag == "" {
			*versionTag = *pgVersion
		}
	}

	if *engineName != "" {
		if _, ok := engine.Lookup(*engineName); !ok {
			fmt.Fprintf(os.Stderr, "\n%s %v\n", ui.StErr.Render("✗"), detect.UnknownEngineErr(*engineName))
			os.Exit(1)
		}
	}

	var dumpPath string
	switch flag.NArg() {
	case 0:
		dataDir, err := defaultDataDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n%s %v\n", ui.StErr.Render("✗"), err)
			os.Exit(1)
		}
		dumpPath, err = ui.PickDump(dataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n%s %v\n", ui.StErr.Render("✗"), err)
			os.Exit(1)
		}
	case 1:
		dumpPath = flag.Arg(0)
	default:
		flag.Usage()
		os.Exit(2)
	}
	if err := run(dumpPath, *versionTag, *engineName, *port, *jobs, *dbName, *keep, !*noCounts); err != nil {
		if errors.Is(err, errInterrupted) {
			fmt.Fprintf(os.Stderr, "\n%s interrompido\n", ui.StWarn.Render("!"))
			os.Exit(130)
		}
		fmt.Fprintf(os.Stderr, "\n%s %v\n", ui.StErr.Render("✗"), err)
		os.Exit(1)
	}
}

// errInterrupted marca a saída por SIGINT/SIGTERM: main imprime
// "interrompido" e sai com 130.
var errInterrupted = errors.New("interrompido")

// interruptedErr traduz um erro para errInterrupted quando o ctx foi
// cancelado (o erro real é consequência do cancelamento); com o ctx vivo,
// devolve err intacto.
func interruptedErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errInterrupted
	}
	return err
}

func step(format string, a ...any) {
	fmt.Printf("%s %s\n", ui.StAccent.Render("==>"), fmt.Sprintf(format, a...))
}

func run(path, versionTag, engineName string, port, jobs int, dbName string, keep, exactCounts bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// SIGINT/SIGTERM cancelam o ctx desde já, antes do Provision: durante o
	// provisionamento, o próprio Provision limpa o container e devolve erro.
	// Depois dele, a sessão viva é fechada aqui (salvo --keep) e o processo
	// sai com 130. mu serializa "cancelar + ler live" contra "gravar live +
	// checar ctx", para um sinal no fim do Provision não fechar a sessão
	// duas vezes nem nenhuma.
	var (
		mu   sync.Mutex
		live engine.Session
	)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	go func() {
		select {
		case <-sigs:
		case <-ctx.Done():
			return // run terminou normalmente
		}
		mu.Lock()
		cancel()
		sess := live
		mu.Unlock()
		if sess == nil {
			return // Provision em andamento: ele limpa e run devolve errInterrupted
		}
		if !keep {
			sess.Close()
		}
		os.Exit(130)
	}()

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	backup, err := detect.InspectDumpAs(abs, engineName)
	if err != nil {
		return err
	}
	if backup.Guessed {
		fmt.Fprintf(os.Stderr, "%s não foi possível identificar o formato do arquivo com confiança; presumindo %s. Use --engine para forçar outra.\n", ui.StWarn.Render("!"), backup.Engine)
	}
	eng, ok := engine.Lookup(backup.Engine)
	if !ok {
		return fmt.Errorf("engine %q não registrada", backup.Engine)
	}

	fmt.Println()
	fmt.Println(ui.StTitle.Render("Verify Backup"))
	fmt.Printf("  %s %s (%s)\n", ui.StLabel.Render("arquivo    :"), abs, engine.HumanSize(backup.Size))
	fmt.Printf("  %s %s / compressão %s\n", ui.StLabel.Render("formato    :"), backup.Format, backup.Compression)
	if backup.OriginDB != "" {
		fmt.Printf("  %s %s\n", ui.StLabel.Render("banco orig.:"), backup.OriginDB)
	}
	fmt.Printf("  %s %s (%s)\n", ui.StLabel.Render("versão     :"), ui.OrDash(backup.Version), eng.Name())
	fmt.Println()

	opts := engine.ProvisionOpts{
		VersionTag: versionTag, Port: port, Jobs: jobs, DBName: dbName,
		Progress: step,
	}
	sess, err := eng.Provision(ctx, backup, opts)
	if err != nil {
		return interruptedErr(ctx, err)
	}

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
			fmt.Printf("\n%s sessão mantida: %s\n", ui.StAccent.Render("==>"), label)
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

	mu.Lock()
	live = sess
	interrupted := ctx.Err() != nil
	mu.Unlock()
	if interrupted {
		// sinal chegou entre o fim do Provision e aqui: o defer acima
		// fecha (ou mantém, com --keep) a sessão.
		return errInterrupted
	}

	if res := sess.Restore(); res != nil {
		if len(res.Errors) == 0 {
			fmt.Printf("%s restore concluído sem erros em %s\n", ui.StOK.Render("✓"), res.Duration.Round(time.Millisecond))
		} else {
			fmt.Printf("%s restore com %d erro(s) em %s\n", ui.StWarn.Render("!"), len(res.Errors), res.Duration.Round(time.Millisecond))
			for i, e := range res.Errors {
				if i == 5 {
					fmt.Printf("    %s\n", ui.StDim.Render(fmt.Sprintf("… mais %d", len(res.Errors)-5)))
					break
				}
				fmt.Printf("    %s\n", truncate(e, 110))
			}
			if res.LogPath != "" {
				fmt.Printf("    %s\n", ui.StDim.Render("log: "+res.LogPath))
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
		ui.NewModel(sess, backup, health, collections),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		return err
	}

	hint := sess.ConnectHint()
	fmt.Printf("\n%s conexão para reusar:\n  %s\n", ui.StAccent.Render("==>"), hint.Shell)
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
