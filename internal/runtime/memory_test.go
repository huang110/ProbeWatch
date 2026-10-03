package runtime

import "testing"

func TestParseMemoryLimit(t *testing.T) {
	for _, tc := range []struct { input string; want int64 }{
		{"150MiB", 150 << 20}, {"256MB", 256 << 20}, {"1GiB", 1 << 30}, {"1024", 1024},
	} {
		got, ok := parseMemoryLimit(tc.input)
		if !ok || got != tc.want { t.Fatalf("parseMemoryLimit(%q) = %d, %v; want %d, true", tc.input, got, ok, tc.want) }
	}
	for _, input := range []string{"", "0", "-1", "nope", "999999999999999999999999GB"} {
		if got, ok := parseMemoryLimit(input); ok { t.Fatalf("parseMemoryLimit(%q) = %d, true; want invalid", input, got) }
	}
}
