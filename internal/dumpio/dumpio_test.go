package dumpio

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMaybeCompressed(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "a.sql")
	if err := os.WriteFile(plain, []byte("SELECT 1;"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte("SELECT 1;"))
	zw.Close()
	gz := filepath.Join(dir, "a.sql.gz")
	if err := os.WriteFile(gz, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	for path, comp := range map[string]string{plain: "none", gz: "gzip"} {
		r, got, err := OpenMaybeCompressed(path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r)
		r.Close()
		if got != comp || string(b) != "SELECT 1;" {
			t.Errorf("%s: compressão %q conteúdo %q", path, got, b)
		}
	}
}
