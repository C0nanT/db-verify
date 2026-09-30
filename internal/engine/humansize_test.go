package engine

import "testing"

func TestHumanSize(t *testing.T) {
	casos := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1 << 20, "1.0 MB"},
		{5 << 30, "5.0 GB"},
	}
	for _, c := range casos {
		if got := HumanSize(c.in); got != c.want {
			t.Errorf("HumanSize(%d) = %q, quero %q", c.in, got, c.want)
		}
	}
}
