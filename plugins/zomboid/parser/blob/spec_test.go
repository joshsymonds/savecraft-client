package blob

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// specDir is the vendored pzdataspec tree this package is a hand-written port
// of; specEntry is the single root the closure is computed from.
const (
	specDir   = "../spec"
	specEntry = "249/iso_object.ksy"
)

// TestSpecClosureIsComplete machine-checks the claim spec/README.md and
// spec/UPSTREAM.txt make: that the vendored tree is the whole transitive import
// closure of the iso_object root, with nothing missing and nothing stray.
func TestSpecClosureIsComplete(t *testing.T) {
	closure := map[string]bool{}
	queue := []string{specEntry}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if closure[current] {
			continue
		}
		closure[current] = true
		for _, target := range ksyImports(t, current) {
			if _, err := os.Stat(filepath.Join(specDir, target)); err != nil {
				t.Errorf("%s imports %s, which is not vendored", current, target)
				continue
			}
			queue = append(queue, target)
		}
	}
	assertSameFiles(t, "vendored", vendoredKSY(t), sortedKeys(closure))
}

// TestSpecReadmeListsTheClosure keeps spec/README.md's file list honest: it must
// name every vendored file and nothing else.
func TestSpecReadmeListsTheClosure(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(specDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, line := range strings.Split(string(source), "\n") {
		if line = strings.TrimSpace(line); strings.HasSuffix(line, ".ksy") && !strings.ContainsAny(line, " \t`") {
			listed = append(listed, line)
		}
	}
	assertSameFiles(t, "listed in README.md", listed, vendoredKSY(t))
}

// ksyImports returns the paths a .ksy file imports, resolved relative to the
// spec root. Kaitai writes them under meta as a YAML block sequence of
// extension-less paths relative to the importing file.
func ksyImports(t *testing.T, file string) []string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(specDir, file))
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	var imports []string
	indent, inBlock := 0, false
	for _, line := range strings.Split(string(source), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "imports:" {
			indent, inBlock = len(line)-len(strings.TrimLeft(line, " ")), true
			continue
		}
		if !inBlock || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		entry, isEntry := strings.CutPrefix(trimmed, "- ")
		if !isEntry || len(line)-len(strings.TrimLeft(line, " ")) <= indent {
			inBlock = false
			continue
		}
		imports = append(imports, path.Join(path.Dir(file), strings.TrimSpace(entry))+".ksy")
	}
	return imports
}

// vendoredKSY lists every .ksy file under spec/, as a path relative to it.
func vendoredKSY(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(specDir, func(p string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(p) != ".ksy" {
			return err
		}
		relative, err := filepath.Rel(specDir, p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// assertSameFiles reports every one-sided difference between two file sets.
func assertSameFiles(t *testing.T, label string, got, want []string) {
	t.Helper()
	in := func(files []string) map[string]bool {
		set := make(map[string]bool, len(files))
		for _, file := range files {
			set[file] = true
		}
		return set
	}
	gotSet, wantSet := in(got), in(want)
	for _, file := range want {
		if !gotSet[file] {
			t.Errorf("%s is missing %s", label, file)
		}
	}
	for _, file := range got {
		if !wantSet[file] {
			t.Errorf("%s has %s, which the closure does not reach", label, file)
		}
	}
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
