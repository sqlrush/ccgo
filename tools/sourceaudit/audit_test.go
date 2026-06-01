package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuditSource(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "src", "types", "present.ts"), "export const x = 1\n")
	mustWrite(t, filepath.Join(root, "src", "main.ts"), `
import { x } from './types/present.js'
import missing from './types/missing.js'
if (feature('BUDDY') && process.env.CLAUDE_CODE_TEST_FLAG) console.log(x, missing)
`)
	mustWrite(t, filepath.Join(root, "assets", "logo.txt"), "logo")

	audit, err := AuditSource(root)
	if err != nil {
		t.Fatal(err)
	}
	if audit.SourceFiles != 2 {
		t.Fatalf("source files = %d", audit.SourceFiles)
	}
	if audit.FeatureGates["BUDDY"] != 1 {
		t.Fatalf("feature gates = %#v", audit.FeatureGates)
	}
	if audit.EnvironmentKeys["CLAUDE_CODE_TEST_FLAG"] != 1 {
		t.Fatalf("env keys = %#v", audit.EnvironmentKeys)
	}
	if len(audit.MissingImports) != 1 || audit.MissingImports[0].Target != "src/types/missing" {
		t.Fatalf("missing imports = %#v", audit.MissingImports)
	}
	if audit.AssetsBytes == 0 {
		t.Fatal("assets bytes not counted")
	}
}

func mustWrite(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
