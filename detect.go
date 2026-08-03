package main

// Detecção de formato genérica, em duas fases (ver SPEC.md, "Detecção"):
//
//  1. abrir o arquivo e descomprimir o cabeçalho se necessário (gzip, zstd,
//     bzip2), lendo só os primeiros ~8 KB — nunca o arquivo inteiro, para um
//     dump gigante não travar o programa;
//  2. perguntar a cada engine registrada quem reconhece o conteúdo e ficar
//     com a de maior confiança (magic bytes > extensão > palpite); empate
//     fica com a primeira registrada.
//
// --engine pula a fase 2 inteira: em vez de perguntar a todo o registro, só
// a engine escolhida é consultada (ela ainda precisa do cabeçalho
// descomprimido para decidir Format/Compression, usados no provisionamento).

import (
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// headerSize é quanto do arquivo (já descomprimido) a detecção lê. Grande o
// bastante para os cabeçalhos que as engines inspecionam, pequeno o
// bastante para nunca se aproximar do tamanho de um dump de verdade.
const headerSize = 8192

// openMaybeCompressed devolve um reader já descomprimido quando necessário.
// Só os magic bytes de compressão são olhados aqui; o corpo é lido sob
// demanda pelo chamador (que, na detecção, para depois de headerSize bytes).
func openMaybeCompressed(path string) (io.ReadCloser, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	br := bufio.NewReader(f)
	magic, _ := br.Peek(4)
	switch {
	case len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
		gz, err := gzip.NewReader(br)
		if err != nil {
			f.Close()
			return nil, "", fmt.Errorf("gzip inválido: %w", err)
		}
		return readCloser{gz, f}, "gzip", nil
	case len(magic) >= 4 && magic[0] == 0x28 && magic[1] == 0xb5 && magic[2] == 0x2f && magic[3] == 0xfd:
		zr, err := zstd.NewReader(br)
		if err != nil {
			f.Close()
			return nil, "", fmt.Errorf("zstd inválido: %w", err)
		}
		return zstdReadCloser{zr, f}, "zstd", nil
	case len(magic) >= 3 && magic[0] == 'B' && magic[1] == 'Z' && magic[2] == 'h':
		return readCloser{bzip2.NewReader(br), f}, "bzip2", nil
	default:
		return readCloser{br, f}, "none", nil
	}
}

type readCloser struct {
	io.Reader
	c io.Closer
}

func (r readCloser) Close() error { return r.c.Close() }

// zstdReadCloser fecha tanto o *zstd.Decoder (que mantém goroutines de
// descompressão) quanto o arquivo subjacente.
type zstdReadCloser struct {
	*zstd.Decoder
	f *os.File
}

func (z zstdReadCloser) Close() error {
	z.Decoder.Close()
	return z.f.Close()
}

// printableStrings extrai as sequências imprimíveis (>=2 chars) de um blob binário.
func printableStrings(b []byte) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() >= 2 {
			out = append(out, cur.String())
		}
		cur.Reset()
	}
	for _, c := range b {
		if c >= 0x20 && c < 0x7f {
			cur.WriteByte(c)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// readHeader abre o arquivo (descomprimindo se preciso) e devolve seu
// tamanho original e os primeiros headerSize bytes já descomprimidos.
func readHeader(path string) (size int64, head []byte, compression string, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, nil, "", err
	}
	r, compression, err := openMaybeCompressed(path)
	if err != nil {
		return 0, nil, "", err
	}
	defer r.Close()

	buf := make([]byte, headerSize)
	n, _ := io.ReadFull(r, buf)
	return st.Size(), buf[:n], compression, nil
}

// chooseEngine roda a fase 2 da detecção contra uma lista explícita de
// engines: pergunta a cada uma e fica com a de maior confiança; empate fica
// com a primeira da lista. Separada de InspectDump para poder ser testada
// com engines fictícias, sem depender do registro global (ver detect_test.go).
func chooseEngine(engines []Engine, head []byte, path string) (Engine, Match, bool) {
	var bestEngine Engine
	var best Match
	for _, e := range engines {
		m, ok := e.Detect(head, path)
		if !ok {
			continue
		}
		if bestEngine == nil || m.Confidence > best.Confidence {
			bestEngine, best = e, m
		}
	}
	return bestEngine, best, bestEngine != nil
}

// noEngineErr monta o erro de "nenhuma engine reconheceu", nomeando as
// engines disponíveis e o que cada uma espera.
func noEngineErr(engines []Engine) error {
	if len(engines) == 0 {
		return fmt.Errorf("nenhuma engine registrada")
	}
	var b strings.Builder
	b.WriteString("nenhuma engine reconheceu o arquivo. engines disponíveis:\n")
	for _, e := range engines {
		fmt.Fprintf(&b, "  - %s: %s\n", e.Name(), e.Expects())
	}
	b.WriteString("use --engine para forçar uma delas, se você sabe do que se trata")
	return fmt.Errorf("%s", b.String())
}

// InspectDump lê o cabeçalho do arquivo (descomprimindo se preciso) e
// pergunta ao registro qual engine o reconhece.
func InspectDump(path string) (*Backup, error) {
	return inspect(path, "")
}

// InspectDumpAs força a engine indicada, pulando a disputa entre engines
// (fase 2 da detecção): só forceEngine é consultada para o cabeçalho já
// descomprimido. forceEngine precisa estar registrada.
func InspectDumpAs(path, forceEngine string) (*Backup, error) {
	if forceEngine == "" {
		return inspect(path, "")
	}
	eng, ok := Lookup(forceEngine)
	if !ok {
		return nil, unknownEngineErr(forceEngine)
	}
	return inspect(path, eng.Name())
}

// unknownEngineErr monta o erro de --engine com um nome que não está
// registrado, listando as engines disponíveis.
func unknownEngineErr(name string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "engine %q não existe. engines disponíveis:\n", name)
	for _, e := range Engines() {
		fmt.Fprintf(&b, "  - %s\n", e.Name())
	}
	return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
}

func inspect(path, forceEngine string) (*Backup, error) {
	size, head, compression, err := readHeader(path)
	if err != nil {
		return nil, err
	}
	b := &Backup{Path: path, Size: size, Compression: compression}

	if forceEngine != "" {
		eng, ok := Lookup(forceEngine)
		if !ok {
			return nil, unknownEngineErr(forceEngine)
		}
		m, _ := eng.Detect(head, path) // forçado: usa o que a engine conseguir extrair, mesmo sem reconhecer
		b.Engine = eng.Name()
		b.Format = m.Format
		b.Version = m.Version
		b.OriginDB = m.OriginDB
		b.Forced = true
		return b, nil
	}

	bestEngine, best, ok := chooseEngine(Engines(), head, path)
	if !ok {
		return nil, noEngineErr(Engines())
	}
	b.Engine = bestEngine.Name()
	b.Format = best.Format
	b.Version = best.Version
	b.OriginDB = best.OriginDB
	b.Guessed = best.Confidence <= ConfidenceGuess
	return b, nil
}

func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
