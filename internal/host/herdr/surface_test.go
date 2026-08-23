package herdr

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// packageFiles parses this package's non-test sources without comments,
// so these guards check what the code *does*, not what it says about
// itself. Test files are excluded on purpose: the scripted fake server has
// to reproduce Herdr's wire shapes faithfully, including the fields the
// adapter must not depend on.
func packageFiles(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, parseErr := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatal("no package sources found")
	}
	return fset, files
}

// declaredNames collects every name this package declares — types, funcs,
// methods, consts, vars, struct fields, interface methods — so a surface
// guard checks what this package publishes rather than what it references
// from elsewhere (io.Writer is a reference, not a claim).
func declaredNames(t *testing.T, fset *token.FileSet, files []*ast.File) []*ast.Ident {
	t.Helper()
	var out []*ast.Ident
	add := func(idents ...*ast.Ident) {
		for _, id := range idents {
			if id != nil && id.IsExported() {
				out = append(out, id)
			}
		}
	}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncDecl:
				add(node.Name)
			case *ast.TypeSpec:
				add(node.Name)
			case *ast.ValueSpec:
				add(node.Names...)
			case *ast.Field:
				add(node.Names...)
			}
			return true
		})
	}
	if len(out) == 0 {
		t.Fatalf("no exported declarations found in %d files", fset.Base())
	}
	return out
}

// Writer presence is refuted at Herdr 0.8.2 and final for 0.8.x: no
// method, event, or record exposes "a human is typing", per-source input
// attribution, or a composer lease. This adapter must therefore expose no
// such claim — not even a hopeful, always-false one, which downstream code
// would read as a supported surface.
func TestNoWriterPresenceSurface(t *testing.T) {
	forbidden := []string{"Writer", "Lease", "Typing", "Attribution", "Composer"}
	fset, files := packageFiles(t)
	for _, ident := range declaredNames(t, fset, files) {
		// "Release" legitimately contains "lease"; take it out of the
		// name before matching rather than weakening the list.
		name := strings.ReplaceAll(ident.Name, "Release", "")
		for _, bad := range forbidden {
			if strings.Contains(name, bad) {
				t.Errorf("%s: declared name %q suggests a writer-presence surface Herdr 0.8.2 does not have",
					fset.Position(ident.Pos()), ident.Name)
			}
		}
	}
}

// At 0.8.2 pane.revision stopped tracking screen content: visible changes
// leave it at zero. Nothing in this adapter may decode it, key on it, or
// request it, so the guard is mechanical rather than a comment.
func TestNoRevisionDependence(t *testing.T) {
	fset, files := packageFiles(t)
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.Ident:
				if strings.EqualFold(node.Name, "revision") {
					t.Errorf("%s: identifier %q — revision is not a change detector at 0.8.2",
						fset.Position(node.Pos()), node.Name)
				}
			case *ast.BasicLit:
				if node.Kind == token.STRING && strings.Contains(strings.ToLower(node.Value), "revision") {
					t.Errorf("%s: string literal %s mentions revision",
						fset.Position(node.Pos()), node.Value)
				}
			}
			return true
		})
	}
}

// Stage 1 stops at four of §5.2's six host interfaces. The two left out —
// terminal reads and the prompt path — are boundaries, not oversights, and
// a stub for either would imply a design nobody has made. The prompt path
// in particular carries 0.8.2's sharpest unresolved hazards (a prompt
// merges into and submits a human's half-typed draft; success does not
// prove submission).
func TestNoPromptOrTerminalSurface(t *testing.T) {
	forbidden := []string{"Prompt", "SendInput", "SendKeys", "SendText", "ReadTerminal", "Repaint"}
	fset, files := packageFiles(t)
	for _, ident := range declaredNames(t, fset, files) {
		for _, bad := range forbidden {
			if strings.Contains(ident.Name, bad) {
				t.Errorf("%s: declared name %q reaches outside the Stage-1 host scope",
					fset.Position(ident.Pos()), ident.Name)
			}
		}
	}
}
