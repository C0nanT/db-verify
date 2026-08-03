package main

// Testes de detecção e das partes de redis.go que não dependem de um Redis
// vivo (Camada 1 — sem Docker). A suíte de conformidade
// (redis_conformance_test.go) cobre o resto do contrato Engine/Session
// contra um Redis de verdade.

import (
	"testing"
	"time"
)

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

// TestRedisDetect_ExtensaoSemMagic caracteriza o fallback de confiança
// média por extensão .rdb, quando o conteúdo não começa com o magic REDIS
// (ex.: cabeçalho cortado bem no início por um backup truncado).
func TestRedisDetect_ExtensaoSemMagic(t *testing.T) {
	m, ok := redisEngine{}.Detect([]byte("lixo qualquer"), "backup.rdb")
	if !ok {
		t.Fatal("esperava a engine redis reconhecer .rdb mesmo sem magic")
	}
	if m.Confidence != ConfidenceExtension {
		t.Errorf("Confidence = %d, want %d (ConfidenceExtension)", m.Confidence, ConfidenceExtension)
	}
	if m.Format != "rdb" {
		t.Errorf("Format = %q, want rdb", m.Format)
	}
}

// TestRedisDetect_NaoReconhece caracteriza a rejeição: sem o magic REDIS e
// sem extensão .rdb, a engine Redis não reivindica o arquivo — evita que
// ela vire uma engine "pega-tudo" competindo com o palpite do Postgres.
func TestRedisDetect_NaoReconhece(t *testing.T) {
	_, ok := redisEngine{}.Detect([]byte("PGDMP qualquer coisa"), "arquivo.dump")
	if ok {
		t.Fatal("esperava a engine redis não reconhecer conteúdo sem sinal nenhum")
	}
}

// TestRedisResolveVersion caracteriza a ordem de precedência da versão da
// imagem: --version-tag explícito > versão do RDB traduzida pela tabela >
// fallback documentado (defaultRedisVersion) quando a versão do RDB está
// ausente ou não é conhecida.
func TestRedisResolveVersion(t *testing.T) {
	cases := []struct {
		name       string
		versionTag string
		rdbVersion string
		want       string
	}{
		{"flag explícita vence tudo", "6.2", "0012", "6.2"},
		{"versão do RDB traduzida pela tabela", "", "0011", "6.2"},
		{"versão do RDB conhecida mais antiga", "", "0006", "2.8"},
		{"versão do RDB desconhecida cai no fallback", "", "0099", defaultRedisVersion},
		{"sem nenhuma informação cai no fallback", "", "", defaultRedisVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redisResolveVersion(tc.versionTag, tc.rdbVersion); got != tc.want {
				t.Errorf("redisResolveVersion(%q, %q) = %q, want %q", tc.versionTag, tc.rdbVersion, got, tc.want)
			}
		})
	}
}

// TestRedisGroupOf caracteriza o agrupamento por prefixo até o primeiro
// separador, incluindo o caso sem separador (ticket 07: "chaves sem
// separador caem num grupo próprio identificável em vez de sumirem").
func TestRedisGroupOf(t *testing.T) {
	cases := []struct{ key, want string }{
		{"user:1", "user"},
		{"user:1:profile", "user"},
		{"session:abc", "session"},
		{"standalone", redisNoPrefixGroup},
		{"", redisNoPrefixGroup},
		{":leading-colon", ""},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			if got := redisGroupOf(tc.key); got != tc.want {
				t.Errorf("redisGroupOf(%q) = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}

// TestRedisGroupPattern caracteriza o comando SCAN nativo exibido na tela —
// precisa ser copiável direto para o redis-cli (ticket 07).
func TestRedisGroupPattern(t *testing.T) {
	pattern, cmd := redisGroupPattern("user")
	if pattern != "user:*" {
		t.Errorf("pattern = %q, want user:*", pattern)
	}
	wantCmd := `SCAN 0 MATCH "user:*" COUNT 500`
	if cmd != wantCmd {
		t.Errorf("cmd = %q, want %q", cmd, wantCmd)
	}

	pattern, cmd = redisGroupPattern(redisNoPrefixGroup)
	if pattern != "*" {
		t.Errorf("pattern = %q, want *", pattern)
	}
	wantCmd = `SCAN 0 MATCH "*" COUNT 500`
	if cmd != wantCmd {
		t.Errorf("cmd = %q, want %q", cmd, wantCmd)
	}
}

// TestRedisFormatTTL caracteriza a formatação de TTL: negativo (sem
// expiração, ou chave sumida) vira texto explícito, positivo vira duração
// arredondada ao segundo.
func TestRedisFormatTTL(t *testing.T) {
	cases := []struct {
		name string
		ttl  time.Duration
		want string
	}{
		{"sem expiração (-1)", -1 * time.Second, "sem TTL"},
		{"chave sumida (-2)", -2 * time.Second, "sem TTL"},
		{"com TTL", 90 * time.Second, "1m30s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redisFormatTTL(tc.ttl); got != tc.want {
				t.Errorf("redisFormatTTL(%v) = %q, want %q", tc.ttl, got, tc.want)
			}
		})
	}
}

// TestSplitRedisCmd caracteriza o tokenizador usado por Query: espaços
// separam argumentos, aspas duplas protegem espaços dentro de um argumento.
func TestSplitRedisCmd(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"simples", "PING", []string{"PING"}},
		{"vários argumentos", "SET foo bar", []string{"SET", "foo", "bar"}},
		{"aspas protegem espaço", `SET foo "bar baz"`, []string{"SET", "foo", "bar baz"}},
		{"espaços múltiplos colapsam", "GET   foo", []string{"GET", "foo"}},
		{"vazio", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitRedisCmd(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("splitRedisCmd(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("splitRedisCmd(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestRedisFormatResult caracteriza o achatamento do retorno solto de
// client.Do em linhas de texto: nil, escalar, lista vazia e lista com
// itens.
func TestRedisFormatResult(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want []string
	}{
		{"nulo", nil, []string{"∅"}},
		{"string", "PONG", []string{"PONG"}},
		{"inteiro", int64(42), []string{"42"}},
		{"lista vazia", []any{}, []string{"(vazio)"}},
		{"lista com itens", []any{"a", "b"}, []string{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redisFormatResult(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("redisFormatResult(%#v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("redisFormatResult(%#v)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}
