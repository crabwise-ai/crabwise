package commandments

import (
	"os"
	"path/filepath"
	"testing"

)

func TestExampleCommandmentsValid(t *testing.T) {
	files, err := filepath.Glob("../../examples/commandments/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no example YAML files found")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			rs, err := LoadYAML(data)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			// Also verify rules compile (regex/glob patterns are valid).
			_, err = compileRules(rs.Commandments)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			t.Logf("%d rules OK", len(rs.Commandments))
		})
	}
}
