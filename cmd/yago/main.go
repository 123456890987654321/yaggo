// Command yago generates Go types, an HTTP server interface, and an HTTP client
// from an OpenAPI 3.x YAML spec.
//
// Install:
//
//	go install github.com/123456890987654321/yago/cmd/yago@latest
//
// Usage:
//
//	yago -spec api.yaml -out ./gen -package api
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"

	"github.com/123456890987654321/yago/internal/gen"
	"github.com/123456890987654321/yago/internal/spec"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type genTask struct {
	filename string
	fn       func(io.Writer, *spec.OpenAPI, string) error
}

// defaultTasks lists the files the generator emits, in deterministic order.
var defaultTasks = []genTask{
	{"types.go", gen.GenerateTypes},
	{"server.go", gen.GenerateServer},
	{"client.go", gen.GenerateClient},
	{"body_types.go", gen.GenerateBodyTypes},
}

// run is the testable entry point. It returns a process exit code so main()
// can stay tiny and side-effect-free except for os.Exit.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("yago", flag.ContinueOnError)
	fs.SetOutput(stderr)
	specFile := fs.String("spec", "", "path to OpenAPI 3.x YAML spec (required)")
	outDir := fs.String("out", ".", "output directory for generated files")
	pkg := fs.String("package", "api", "Go package name for generated files")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *specFile == "" {
		fmt.Fprintln(stderr, "error: -spec is required")
		fs.Usage()
		return 1
	}

	api, err := spec.Parse(*specFile)
	if err != nil {
		fmt.Fprintf(stderr, "yago: parsing spec: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "yago: creating output dir: %v\n", err)
		return 1
	}

	for _, t := range defaultTasks {
		var buf bytes.Buffer
		if err := t.fn(&buf, api, *pkg); err != nil {
			fmt.Fprintf(stderr, "yago: generating %s: %v\n", t.filename, err)
			return 1
		}
		src := buf.Bytes()
		if len(bytes.TrimSpace(src)) == 0 {
			continue
		}
		out := filepath.Join(*outDir, t.filename)
		formatted, err := format.Source(src)
		if err != nil {
			// Write unformatted so the user can debug the broken output.
			_ = os.WriteFile(out, src, 0o644)
			fmt.Fprintf(stderr, "yago: formatting %s: %v\n(unformatted file written for debugging)\n", t.filename, err)
			return 1
		}
		if err := os.WriteFile(out, formatted, 0o644); err != nil {
			fmt.Fprintf(stderr, "yago: writing %s: %v\n", out, err)
			return 1
		}
		fmt.Fprintf(stdout, "wrote %s\n", out)
	}
	return 0
}
