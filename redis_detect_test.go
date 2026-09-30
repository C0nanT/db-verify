package main

// Detecção do Redis contra as fixtures reais em testdata/headers: precisa do
// conjunto de engines montado na raiz, então não mora em engines/redis.

import "testing"

// TestRedisDetect_Magic caracteriza o reconhecimento pelo magic "REDIS" +
// versão do formato, usando um dump.rdb real (testdata/headers/redis.rdb).
func TestRedisDetect_Magic(t *testing.T) {
	info, err := InspectDump("testdata/headers/redis.rdb")
	if err != nil {
		t.Fatalf("InspectDump: %v", err)
	}
	if info.Engine != "redis" {
		t.Fatalf("Engine = %q, want redis", info.Engine)
	}
	if info.Format != "rdb" {
		t.Errorf("Format = %q, want rdb", info.Format)
	}
	if info.Guessed {
		t.Errorf("esperava Guessed=false para magic bytes")
	}
	if info.Version != "0012" {
		t.Errorf("Version = %q, want 0012", info.Version)
	}
}

// TestRedisDetect_Gzip caracteriza a detecção através de gzip: o cabeçalho é
// descomprimido antes de a engine olhar para ele.
func TestRedisDetect_Gzip(t *testing.T) {
	info, err := InspectDump("testdata/headers/redis.rdb.gz")
	if err != nil {
		t.Fatalf("InspectDump: %v", err)
	}
	if info.Compression != "gzip" {
		t.Errorf("Compression = %q, want gzip", info.Compression)
	}
	if info.Engine != "redis" {
		t.Errorf("Engine = %q, want redis", info.Engine)
	}
	if info.Version != "0012" {
		t.Errorf("Version = %q, want 0012", info.Version)
	}
}
