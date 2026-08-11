package testutil_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const modulePath = "github.com/NerdMeNot/flint"

// Every third-party import must be a DIRECT requirement in go.mod.
//
// This is the general form of a trap that has already bitten twice in one
// sitting: `task fmt` runs goimports over the tree, and goimports resolves a
// bare identifier by searching the whole module graph — every transitive
// dependency included. It picked github.com/pingcap/log (a dependency of sqlc's
// tree) for a bare `log` in two files. That one happened to fail the build
// because its API differs; a twin with a compatible signature would have
// compiled and shipped.
//
// Mis-resolutions always land on an INDIRECT dependency, because the direct ones
// are the packages someone actually chose. So rather than enumerating twins as
// they are discovered, this asserts the invariant they all violate. It is also
// self-maintaining: legitimately adding a package means `go get` promotes it to
// a direct require, and this passes.
//
// depguard covers what this cannot — stdlib twins like `log`, where there is no
// go.mod entry to check.
func TestImports_ThirdPartyMustBeDirectDependencies(t *testing.T) {
	root := repoRoot(t)
	direct := directRequires(t, filepath.Join(root, "go.mod"))

	type violation struct{ file, imp string }
	var bad []violation

	for _, dir := range []string{"cmd", "internal", "pkg"} {
		walkGoFiles(t, filepath.Join(root, dir), func(path string, imports []string) {
			for _, imp := range imports {
				if isStdlib(imp) || strings.HasPrefix(imp, modulePath) {
					continue
				}
				if moduleOf(imp, direct) == "" {
					rel, _ := filepath.Rel(root, path)
					bad = append(bad, violation{rel, imp})
				}
			}
		})
	}

	for _, v := range bad {
		t.Errorf("%s imports %q, which is an indirect dependency.\n"+
			"    Either it is a wrong-twin import goimports resolved from the module graph\n"+
			"    (check for a first-party package with the same short name), or it is a\n"+
			"    deliberate new dependency — in which case run: go get %s", v.file, v.imp, v.imp)
	}
}

// repoRoot walks up from the working directory to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "no go.mod found above the working directory")
		dir = parent
	}
}

// directRequires returns the module paths required WITHOUT an `// indirect`
// marker. Parsed textually to keep this check free of its own dependencies.
func directRequires(t *testing.T, goMod string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(goMod)
	require.NoError(t, err)

	direct := map[string]bool{}
	inBlock := false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "require ("):
			inBlock = true
			continue
		case inBlock && trimmed == ")":
			inBlock = false
			continue
		}
		if strings.Contains(trimmed, "// indirect") || trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if !inBlock {
			if !strings.HasPrefix(trimmed, "require ") {
				continue
			}
			trimmed = strings.TrimPrefix(trimmed, "require ")
		}
		if fields := strings.Fields(trimmed); len(fields) >= 2 {
			direct[fields[0]] = true
		}
	}
	require.NotEmpty(t, direct, "parsed no direct requirements — the go.mod parser is wrong")
	return direct
}

// moduleOf returns the longest direct-require prefix of imp, or "" if none.
func moduleOf(imp string, direct map[string]bool) string {
	best := ""
	for mod := range direct {
		if imp == mod || strings.HasPrefix(imp, mod+"/") {
			if len(mod) > len(best) {
				best = mod
			}
		}
	}
	return best
}

// isStdlib reports whether an import path is from the standard library: its
// first segment carries no dot, so it is not a domain.
func isStdlib(imp string) bool {
	first, _, _ := strings.Cut(imp, "/")
	return !strings.Contains(first, ".")
}

func walkGoFiles(t *testing.T, dir string, fn func(path string, imports []string)) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Generated protobuf output and vendored reference repos are not ours.
			if name := d.Name(); name == "testdata" || name == "protogen" || name == "_reference" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			return nil // not our concern here; the build catches unparseable files
		}
		imports := make([]string, 0, len(f.Imports))
		for _, spec := range f.Imports {
			p, uerr := strconv.Unquote(spec.Path.Value)
			if uerr == nil {
				imports = append(imports, p)
			}
		}
		fn(path, imports)
		return nil
	})
	require.NoError(t, err)
}
