// Package dumpio abre arquivos de backup que podem estar comprimidos
// (gzip, zstd, bzip2), devolvendo um reader já descomprimido. Não importa
// nenhum outro pacote do projeto.
package dumpio

import (
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

// OpenMaybeCompressed devolve um reader já descomprimido quando necessário.
// Só os magic bytes de compressão são olhados aqui; o corpo é lido sob
// demanda pelo chamador (que, na detecção, para depois de headerSize bytes).
func OpenMaybeCompressed(path string) (io.ReadCloser, string, error) {
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
