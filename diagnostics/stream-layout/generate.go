// Layout-only experiment generator. Never moves init, vars, or types.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

var wanted = []string{"fn.bodyLoop", "fn.emitPlain", "fn.condenseBinary", "fn.materialize", "fn.setLocal", "fn.opEnd", "scanLoopCallFreeDetails", "byteBodyScanner.scanExpr"}

type decl struct {
	Key, File, Raw, AST string
	Start, End          int
	Index               int
	Imports             map[string]string
}
type edit struct {
	lo, hi int
	text   string
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}
func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func key(f *ast.FuncDecl) string {
	if f.Recv == nil {
		return f.Name.Name
	}
	t := f.Recv.List[0].Type
	if p, ok := t.(*ast.StarExpr); ok {
		t = p.X
	}
	n, ok := t.(*ast.Ident)
	if !ok {
		panic("unexpected receiver")
	}
	return n.Name + "." + f.Name.Name
}
func asthash(n ast.Node) string {
	var b bytes.Buffer
	check(ast.Fprint(&b, nil, n, func(n string, v reflect.Value) bool {
		return n != "Obj" && n != "Scope" && n != "Unresolved" && v.Type() != reflect.TypeOf(token.Pos(0))
	}))
	return hash(b.Bytes())
}
func selectors(n ast.Node) map[string]bool {
	m := map[string]bool{}
	ast.Inspect(n, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := s.X.(*ast.Ident); ok {
				m[id.Name] = true
			}
		}
		return true
	})
	return m
}
func importName(i *ast.ImportSpec) string {
	if i.Name != nil {
		return i.Name.Name
	}
	p, e := strconv.Unquote(i.Path.Value)
	check(e)
	return filepath.Base(p)
}
func imports(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, i := range f.Imports {
		n := importName(i)
		if n == "." {
			panic("dot import")
		}
		var b bytes.Buffer
		check(format.Node(&b, token.NewFileSet(), i))
		m[n] = b.String()
	}
	return m
}
func apply(b []byte, es []edit) []byte {
	sort.Slice(es, func(i, j int) bool { return es[i].lo > es[j].lo })
	prev := len(b)
	for _, e := range es {
		if e.hi > prev {
			panic("overlap")
		}
		b = append(append(append([]byte{}, b[:e.lo]...), []byte(e.text)...), b[e.hi:]...)
		prev = e.lo
	}
	return b
}
func inventory(dir string) map[string]int {
	m := map[string]int{}
	files, e := filepath.Glob(filepath.Join(dir, "*.go"))
	check(e)
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		fs := token.NewFileSet()
		f, e := parser.ParseFile(fs, p, nil, parser.ParseComments)
		check(e)
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok {
				m[key(fn)+":"+asthash(fn)]++
			}
		}
	}
	return m
}

// Audit remaining declarations in original file order, init functions, and
// package import dependency set independently from moved function inventory.
func nonfuncInventory(dir string) (map[string][]string, []string) {
	m := map[string][]string{}
	deps := map[string]bool{}
	files, e := filepath.Glob(filepath.Join(dir, "*.go"))
	check(e)
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		fs := token.NewFileSet()
		f, e := parser.ParseFile(fs, p, nil, parser.ParseComments)
		check(e)
		for _, i := range f.Imports {
			deps[i.Path.Value] = true
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok {
				if fn.Name.Name != "init" {
					continue
				}
			} else if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
				continue
			}
			m[filepath.Base(p)] = append(m[filepath.Base(p)], asthash(d))
		}
	}
	names := []string{}
	for n := range deps {
		names = append(names, n)
	}
	sort.Strings(names)
	return m, names
}
func main() {
	if len(os.Args) != 4 {
		panic("generate ROOT hot|neutral MANIFEST")
	}
	root, arm, out := os.Args[1], os.Args[2], os.Args[3]
	if arm != "hot" && arm != "neutral" {
		panic(arm)
	}
	dir := filepath.Join(root, "src/core/compiler/backend/railshot/amd64")
	before := inventory(dir)
	nonfuncBefore, importsBefore := nonfuncInventory(dir)
	files, e := filepath.Glob(filepath.Join(dir, "*.go"))
	check(e)
	ds := []decl{}
	changed := map[string][]edit{}
	payload := map[string][]byte{}
	header := ""
	rank := map[string]int{}
	for i, k := range wanted {
		rank[k] = i
	}
	seen := map[string]bool{}
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		b, e := os.ReadFile(p)
		check(e)
		fs := token.NewFileSet()
		f, e := parser.ParseFile(fs, p, b, parser.ParseComments)
		check(e)
		im := imports(f)
		selected := map[*ast.FuncDecl]bool{}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			k := key(fn)
			idx, ok := rank[k]
			if !ok {
				continue
			}
			if seen[k] {
				panic("duplicate " + k)
			}
			seen[k] = true
			h := string(b[:fs.Position(f.Package).Offset])
			if !strings.Contains(h, "//go:build amd64\n") {
				panic("unexpected build constraint")
			}
			if header == "" {
				header = h
			} else if h != header {
				panic("header mismatch")
			}
			lo := fs.Position(fn.Pos()).Offset
			if fn.Doc != nil {
				lo = fs.Position(fn.Doc.Pos()).Offset
			}
			hi := fs.Position(fn.End()).Offset
			uses := selectors(fn)
			mi := map[string]string{}
			for n, s := range im {
				if uses[n] || (n == "unsafe" && strings.Contains(string(b[lo:hi]), "//go:linkname")) {
					mi[n] = s
				}
			}
			ds = append(ds, decl{k, filepath.Base(p), string(b[lo:hi]), asthash(fn), lo, hi, idx, mi})
			changed[p] = append(changed[p], edit{lo, hi, ""})
			payload[p] = b
			selected[fn] = true
		}
		if len(selected) == 0 {
			continue
		}
		remaining := map[string]bool{}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && selected[fn] {
				continue
			}
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
				continue
			}
			for n := range selectors(d) {
				remaining[n] = true
			}
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.IMPORT {
				continue
			}
			keep := []ast.Spec{}
			for _, s := range gd.Specs {
				i := s.(*ast.ImportSpec)
				n := importName(i)
				if n == "_" || remaining[n] || (n == "unsafe" && strings.Contains(string(b), "//go:linkname")) {
					keep = append(keep, s)
				}
			}
			if len(keep) == len(gd.Specs) {
				continue
			}
			importStart, importEnd := fs.Position(gd.Pos()).Offset, fs.Position(gd.End()).Offset
			gd.Specs = keep
			var ib bytes.Buffer
			if len(keep) > 0 {
				check(format.Node(&ib, fs, gd))
			}
			changed[p] = append(changed[p], edit{importStart, importEnd, ib.String()})
		}
	}
	if len(ds) != len(wanted) {
		panic(fmt.Sprintf("found %d", len(ds)))
	}
	if arm == "hot" {
		sort.Slice(ds, func(i, j int) bool { return ds[i].Index < ds[j].Index })
	} else {
		sort.Slice(ds, func(i, j int) bool {
			if ds[i].File != ds[j].File {
				return ds[i].File < ds[j].File
			}
			return ds[i].Start < ds[j].Start
		})
	}
	im := map[string]string{}
	for _, d := range ds {
		for n, s := range d.Imports {
			if old, ok := im[n]; ok && old != s {
				panic("import alias collision")
			}
			im[n] = s
		}
	}
	names := []string{}
	for n := range im {
		names = append(names, n)
	}
	sort.Strings(names)
	var nb strings.Builder
	nb.WriteString(header + "package amd64\n\nimport (\n")
	for _, n := range names {
		nb.WriteString(im[n] + "\n")
	}
	nb.WriteString(")\n\n")
	for _, d := range ds {
		nb.WriteString(d.Raw + "\n\n")
	}
	newb, e := format.Source([]byte(nb.String()))
	check(e)
	name := "a00_layout_hot.go"
	if arm == "neutral" {
		name = "zzz_layout_control.go"
	}
	path := filepath.Join(dir, name)
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		panic("target exists")
	}
	for p, es := range changed {
		b := apply(payload[p], es)
		b, e = format.Source(b)
		check(e)
		check(os.WriteFile(p, b, 0644))
	}
	check(os.WriteFile(path, newb, 0644))
	after := inventory(dir)
	nonfuncAfter, importsAfter := nonfuncInventory(dir)
	if !reflect.DeepEqual(nonfuncBefore, nonfuncAfter) || !reflect.DeepEqual(importsBefore, importsAfter) {
		panic("NONFUNCTION/INIT/IMPORT DEPENDENCY INVENTORY MISMATCH")
	}
	if !reflect.DeepEqual(before, after) {
		panic("FUNCTION AST INVENTORY MISMATCH")
	}
	manifest := map[string]any{"arm": arm, "selected": ds, "before_inventory": before, "nonfunction_init_inventory": nonfuncBefore, "import_dependency_set": importsBefore, "after_inventory": after, "target": name, "source": "b5ab03f9", "policy": "same 8 functions; unchanged AST incl comments; no init/vars/types moved"}
	b, e := json.MarshalIndent(manifest, "", "  ")
	check(e)
	check(os.WriteFile(out, b, 0644))
	fmt.Printf("%s: %d declarations; full function AST inventory equal\n", arm, len(ds))
}
