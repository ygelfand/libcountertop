package inits

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

var skipped = map[string]bool{
	"third_party":  true,
	"vendor":       true,
	"testdata":     true,
	"node_modules": true,
}

func Scattered(root string) ([]string, error) {
	dirs := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (skipped[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			dirs[filepath.Dir(path)] = append(dirs[filepath.Dir(path)], path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var found []string
	for _, files := range dirs {
		more, err := check(root, files)
		if err != nil {
			return nil, err
		}
		found = append(found, more...)
	}
	sort.Strings(found)
	return found, nil
}

func check(root string, paths []string) ([]string, error) {
	fset := token.NewFileSet()
	owner := map[string]map[string]string{}
	parsed := make([]*ast.File, 0, len(paths))
	for _, p := range paths {
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, f)
		vars := owner[f.Name.Name]
		if vars == nil {
			vars = map[string]string{}
			owner[f.Name.Name] = vars
		}
		for _, decl := range f.Decls {
			g, ok := decl.(*ast.GenDecl)
			if !ok || g.Tok != token.VAR {
				continue
			}
			for _, spec := range g.Specs {
				v, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, n := range v.Names {
					vars[n.Name] = p
				}
			}
		}
	}
	var found []string
	for i, f := range parsed {
		vars := owner[f.Name.Name]
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "init" || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				a, ok := n.(*ast.AssignStmt)
				if !ok || a.Tok == token.DEFINE {
					return true
				}
				for _, lhs := range a.Lhs {
					name := rootName(lhs)
					from, ok := vars[name]
					if !ok || from == paths[i] {
						continue
					}
					rel, _ := filepath.Rel(root, fset.Position(a.Pos()).Filename)
					decl, _ := filepath.Rel(root, from)
					found = append(found, fmt.Sprintf(
						"%s:%d: init writes %s, declared in %s",
						rel,
						fset.Position(a.Pos()).Line,
						name,
						decl,
					))
				}
				return true
			})
		}
	}
	return found, nil
}

func rootName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x.Name
		case *ast.IndexExpr:
			e = x.X
		case *ast.SelectorExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		default:
			return ""
		}
	}
}
