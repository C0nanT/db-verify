package main

import (
	"strings"
	"testing"
)

func TestPortFlagUsageDescribesExplicitThenEngineWindow(t *testing.T) {
	t.Parallel()
	if strings.Contains(portFlagUsage, "55432") {
		t.Fatalf("help de --port ainda cita 55432: %q", portFlagUsage)
	}
	for _, want := range []string{"primeira tentativa", "janela"} {
		if !strings.Contains(portFlagUsage, want) {
			t.Fatalf("help de --port sem %q: %q", want, portFlagUsage)
		}
	}
}
