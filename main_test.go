package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteOptions(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "fluent", "v1", "options.proto")

	for _, want := range []string{"created", "unchanged"} {
		got, err := writeOptions(root)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("status = %q, want %q", got, want)
		}
	}

	if err := os.WriteFile(dst, []byte("// stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := writeOptions(root); err != nil || got != "updated" {
		t.Errorf("status = %q, %v; want updated", got, err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, optionsProto) {
		t.Error("file content does not match the embedded options")
	}
}

func TestEmbeddedOptionsMatchesSource(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("proto/" + optionsPath))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(src, optionsProto) {
		t.Error("embedded options.proto differs from proto/fluent/v1/options.proto")
	}
}
