// Command protoc-gen-go-fluent is a protoc/buf plugin that generates fluent,
// paginated Go clients from gRPC services annotated with fluent/v1/options.proto.
//
// Options:
//
//	import_path  Go import path of the output package (required)
//	package      Go package name (default: last element of import_path)
//	filename     output file name (default "fluent.gen.go")
//	client       generate the Client type, Dial, New and With (default true)
//
// Run with strategy "all" so resources declared in different files can reference each other.
//
// Commands (when run directly rather than by protoc/buf):
//
//	protoc-gen-go-fluent init [proto-root]   write fluent/v1/options.proto under proto-root (default "proto")
//	protoc-gen-go-fluent --version
package main

import (
	"bytes"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/wakelesstuna/protoc-gen-go-fluent/internal/generator"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// optionsProto is the options file matching this build of the plugin.
//
//go:embed proto/fluent/v1/options.proto
var optionsProto []byte

// optionsPath is where init writes the options, relative to the proto root.
// It must match the import path used in .proto files: import "fluent/v1/options.proto".
const optionsPath = "fluent/v1/options.proto"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("protoc-gen-go-fluent", version)
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "init" {
		if err := runInit(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "protoc-gen-go-fluent init:", err)
			os.Exit(1)
		}
		return
	}

	var flags flag.FlagSet
	importPath := flags.String("import_path", "", "Go import path of the output package")
	pkg := flags.String("package", "", "Go package name")
	filename := flags.String("filename", "fluent.gen.go", "output file name")
	client := flags.Bool("client", true, "generate the Client type")

	protogen.Options{ParamFunc: flags.Set}.Run(func(p *protogen.Plugin) error {
		p.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
		if *importPath == "" {
			return errors.New("protoc-gen-go-fluent: import_path option is required")
		}
		if *pkg == "" {
			*pkg = path.Base(*importPath)
		}
		return generator.Generate(p, generator.Config{
			PackageName: *pkg,
			ImportPath:  protogen.GoImportPath(*importPath),
			Filename:    *filename,
			Client:      *client,
		})
	})
}

func runInit(args []string) error {
	if len(args) > 1 {
		return errors.New("usage: protoc-gen-go-fluent init [proto-root]")
	}
	root := "proto"
	if len(args) == 1 {
		root = args[0]
	}
	status, err := writeOptions(root)
	if err != nil {
		return err
	}
	fmt.Printf("%s %s\n", status, filepath.Join(root, optionsPath))
	return nil
}

// writeOptions writes the embedded options file under root and reports whether
// it was "created", "updated" or "unchanged".
func writeOptions(root string) (string, error) {
	dst := filepath.Join(root, filepath.FromSlash(optionsPath))
	existing, err := os.ReadFile(dst)
	switch {
	case err == nil && bytes.Equal(existing, optionsProto):
		return "unchanged", nil
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return "", err
	}
	status := "updated"
	if err != nil {
		status = "created"
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, optionsProto, 0o644); err != nil {
		return "", err
	}
	return status, nil
}
