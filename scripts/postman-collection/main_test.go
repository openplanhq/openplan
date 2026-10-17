package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The committed collection must be exactly what the spec generates. A spec
// change that skips `make postman-collection` fails here, rather than leaving
// the collection describing an API that no longer exists.
func TestCollectionMatchesSpec(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	want, err := generate(spec)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	outDir := filepath.Join("..", "..", "postman")
	got := map[string][]byte{}
	for _, owned := range ownedPaths {
		err := filepath.WalkDir(filepath.Join(outDir, owned), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(outDir, path)
			if err != nil {
				return err
			}
			got[filepath.ToSlash(relative)] = content
			return nil
		})
		if err != nil {
			t.Fatalf("read committed collection: %v", err)
		}
	}

	var stale []string
	for path, content := range want {
		if !bytes.Equal(got[path], content) {
			stale = append(stale, path)
		}
	}
	for path := range got {
		if _, ok := want[path]; !ok {
			stale = append(stale, path)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Fatalf("postman/ is out of date with docs/openapi.yaml; run `make postman-collection`. Differing files:\n%v", stale)
	}
}

// Generating twice gives the same bytes, so the test above can only fail on
// a real change.
func TestGenerateIsDeterministic(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	first, err := generate(spec)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	second, err := generate(spec)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for path, content := range first {
		if !bytes.Equal(second[path], content) {
			t.Fatalf("%s differs between two runs", path)
		}
	}
}

// Every operation in the spec becomes a request, so a new route cannot be
// left out of the collection.
func TestEveryOperationIsARequest(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	doc, err := parseSpec(spec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	model, err := buildCollection(doc)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	operations := 0
	for _, entry := range pairs(get(doc.root, "paths")) {
		for _, op := range pairs(entry[1]) {
			if isMethod(op[0].Value) || isMethod(upper(op[0].Value)) {
				operations++
			}
		}
	}
	requests := 0
	for _, f := range model.folders {
		if f.name != "Contract checks" {
			requests += len(f.requests)
		}
	}
	if requests != operations {
		t.Fatalf("collection has %d requests for %d spec operations", requests, operations)
	}
}

func upper(s string) string { return string(bytes.ToUpper([]byte(s))) }

// The collection test exempts routes from the problem-document check by
// path. A route the spec documents with a non-problem error must be on that
// list, and nothing else may be, or a regression to plain JSON would pass.
func TestNonProblemErrorPathsMatchTheSpec(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	doc, err := parseSpec(spec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var documented []string
	for _, pathEntry := range pairs(get(doc.root, "paths")) {
		nonProblem := false
		for _, opEntry := range pairs(pathEntry[1]) {
			if !isMethod(strings.ToUpper(opEntry[0].Value)) {
				continue
			}
			for _, response := range pairs(get(opEntry[1], "responses")) {
				if status, err := strconv.Atoi(response[0].Value); err != nil || status < 400 {
					continue
				}
				resolved, err := doc.resolve(response[1])
				if err != nil {
					t.Fatal(err)
				}
				for _, media := range pairs(get(resolved, "content")) {
					if media[0].Value != "application/problem+json" {
						nonProblem = true
					}
				}
			}
		}
		if nonProblem {
			documented = append(documented, pathEntry[0].Value)
		}
	}
	exempt := slices.Sorted(slices.Values(nonProblemErrorPaths))
	slices.Sort(documented)
	if !slices.Equal(documented, exempt) {
		t.Fatalf("spec documents non-problem errors on %v, but the collection test exempts %v", documented, exempt)
	}
	// The check must not be gated on the response's media type, or an error
	// served as application/json would skip it.
	if strings.Contains(collectionTest, "contentType.includes") {
		t.Fatal("collection test gates the problem check on the content type")
	}
}
