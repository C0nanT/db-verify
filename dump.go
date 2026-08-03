package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// DumpInfo descreve o que conseguimos deduzir do arquivo de backup.
type DumpInfo struct {
	Path        string
	Size        int64
	Format      string // custom | tar | plain
	Compression string // none | gzip
	OriginDB    string
	PGMajor     string // versão do servidor que gerou o dump ("" se desconhecida)
}

var (
	reHeaderVersion = regexp.MustCompile(`^(\d{1,2})\.\d+`)
	rePlainVersion  = regexp.MustCompile(`Dumped from database version (\d{1,2})`)
	reConnect       = regexp.MustCompile(`(?m)^\\connect (\S+)`)
)

// openMaybeCompressed devolve um reader já descomprimido quando necessário.
func openMaybeCompressed(path string) (io.ReadCloser, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	br := bufio.NewReader(f)
	magic, _ := br.Peek(2)
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			f.Close()
			return nil, "", fmt.Errorf("gzip inválido: %w", err)
		}
		return readCloser{gz, f}, "gzip", nil
	}
	return readCloser{br, f}, "none", nil
}

type readCloser struct {
	io.Reader
	c io.Closer
}

func (r readCloser) Close() error { return r.c.Close() }

// InspectDump lê o cabeçalho e identifica formato, banco de origem e versão.
func InspectDump(path string) (*DumpInfo, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	r, compression, err := openMaybeCompressed(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	head := make([]byte, 8192)
	n, _ := io.ReadFull(r, head)
	head = head[:n]

	info := &DumpInfo{Path: path, Size: st.Size(), Compression: compression}

	switch {
	case bytes.HasPrefix(head, []byte("PGDMP")):
		info.Format = "custom"
		// Cabeçalho binário: PGDMP, banco, versão do pg_dump, versão do servidor.
		parts := printableStrings(head[:min(512, len(head))])
		if len(parts) > 1 {
			info.OriginDB = parts[1]
		}
		for _, p := range parts {
			if m := reHeaderVersion.FindStringSubmatch(p); m != nil {
				info.PGMajor = m[1]
				break
			}
		}
	case bytes.Contains(head, []byte("PostgreSQL database dump")):
		info.Format = "plain"
		if m := rePlainVersion.FindSubmatch(head); m != nil {
			info.PGMajor = string(m[1])
		}
		if m := reConnect.FindSubmatch(head); m != nil {
			info.OriginDB = strings.Trim(string(m[1]), `";`)
		}
	case strings.HasSuffix(strings.ToLower(path), ".tar"):
		info.Format = "tar"
	default:
		info.Format = "plain" // palpite: SQL puro
	}
	return info, nil
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
