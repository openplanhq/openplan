// Command postman-collection writes the Postman collection for the openplan
// API from docs/openapi.yaml.
//
// The spec is the contract, so the collection is derived from it rather than
// kept by hand: every operation becomes a request, with the spec's summary,
// description, example body and documented responses. What the spec cannot
// say — which response id the next request needs, how long to poll an
// asynchronous run, which negative cases pin down the error envelope — lives
// in this package's overrides and contract checks.
//
// TestCollectionMatchesSpec fails when the committed files differ from what
// this writes, so a spec change that is not followed by `make
// postman-collection` fails CI instead of leaving the collection stale.
//
// It writes two forms of the same collection: Postman's git-native v3 YAML
// under postman/collections, which the Postman CLI lints and runs, and a v2.1
// JSON export under postman/export, which the Postman app imports by drag and
// drop.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"sort"
)

func main() {
	specPath := flag.String("spec", "docs/openapi.yaml", "the OpenAPI document to generate from")
	outDir := flag.String("out", "postman", "the directory the collection is written under")
	flag.Parse()

	spec, err := os.ReadFile(*specPath)
	if err != nil {
		log.Fatalf("read spec: %v", err)
	}
	files, err := generate(spec)
	if err != nil {
		log.Fatalf("generate: %v", err)
	}
	if err := writeFiles(*outDir, files); err != nil {
		log.Fatalf("write: %v", err)
	}
	fmt.Printf("wrote %d files under %s\n", len(files), *outDir)
}

// ownedPaths are the paths under the output directory this command owns. It
// removes them before writing, so a request the spec dropped does not linger,
// and leaves anything else under postman/ alone.
var ownedPaths = []string{
	filepath.Join("collections", collectionName),
	filepath.Join("environments", environmentName+".yaml"),
	"export",
}

func writeFiles(outDir string, files map[string][]byte) error {
	for _, owned := range ownedPaths {
		if err := os.RemoveAll(filepath.Join(outDir, owned)); err != nil {
			return err
		}
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		full := filepath.Join(outDir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(full, files[path], 0o644); err != nil { //nolint:gosec // a collection is meant to be read
			return err
		}
	}
	return nil
}

// generate turns the OpenAPI document into every file the collection is made
// of, keyed by path relative to the output directory.
func generate(spec []byte) (map[string][]byte, error) {
	doc, err := parseSpec(spec)
	if err != nil {
		return nil, err
	}
	model, err := buildCollection(doc)
	if err != nil {
		return nil, err
	}
	files, err := renderV3(model)
	if err != nil {
		return nil, err
	}
	export, err := renderV21(model)
	if err != nil {
		return nil, err
	}
	maps.Copy(files, export)
	for path, content := range files {
		// Every file ends in exactly one newline, whatever the encoder did.
		files[path] = append(bytes.TrimRight(content, "\n"), '\n')
	}
	return files, nil
}
