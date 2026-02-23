package commandments

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "commandments", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestLoadYAML_ValidFixtures(t *testing.T) {
	fixtures := []string{"valid-basic.yaml", "valid-all-matchers.yaml"}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			rs, err := LoadYAML(readFixture(t, fixture))
			if err != nil {
				t.Fatalf("expected valid fixture, got error: %v", err)
			}
			if len(rs.Commandments) == 0 {
				t.Fatal("expected commandments")
			}
		})
	}
}

func TestLoadYAML_InvalidFixtures(t *testing.T) {
	fixtures := []string{
		"invalid-bad-regex.yaml",
		"invalid-missing-name.yaml",
		"invalid-bad-enforcement.yaml",
		"invalid-bad-version.yaml",
		"invalid-too-many-rules.yaml",
	}

	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			if _, err := LoadYAML(readFixture(t, fixture)); err == nil {
				t.Fatalf("expected fixture %s to fail", fixture)
			}
		})
	}
}

func TestLoadYAML_PatternLengthCap(t *testing.T) {
	pattern := strings.Repeat("a", MaxPatternChars+1)
	input := fmt.Sprintf(`
version: "1"
commandments:
  - name: too-long-pattern
    enforcement: warn
    match:
      arguments:
        type: regex
        pattern: "%s"
`, pattern)

	if _, err := LoadYAML([]byte(input)); err == nil {
		t.Fatal("expected pattern length validation error")
	}
}

func TestLoadYAML_TotalPatternCap(t *testing.T) {
	values := make([]string, 0, MaxCompiledPatterns+1)
	for i := 0; i < MaxCompiledPatterns+1; i++ {
		values = append(values, fmt.Sprintf("v%d", i))
	}

	b := &strings.Builder{}
	b.WriteString("version: \"1\"\n")
	b.WriteString("commandments:\n")
	b.WriteString("  - name: too-many-patterns\n")
	b.WriteString("    enforcement: warn\n")
	b.WriteString("    match:\n")
	b.WriteString("      model:\n")
	b.WriteString("        type: list\n")
	b.WriteString("        op: in\n")
	b.WriteString("        values:\n")
	for _, v := range values {
		b.WriteString("          - ")
		b.WriteString(v)
		b.WriteString("\n")
	}

	if _, err := LoadYAML([]byte(b.String())); err == nil {
		t.Fatal("expected total pattern cap validation error")
	}
}
