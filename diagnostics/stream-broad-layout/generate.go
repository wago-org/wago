// Fixed-manifest source-order experiment; no semantic edits or init/var movement.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type Row struct {
	Symbol    string   `json:"symbol"`
	Package   string   `json:"package"`
	Hint      string   `json:"decl_key_hint"`
	Flat      int64    `json:"flat_cpu_ns"`
	Inclusive int64    `json:"inclusive_cpu_ns"`
	Notes     []string `json:"expansion_notes"`
}
type Manifest struct {
	Rows []Row `json:"rows"`
}
type Source struct {
	Path, Header, Suffix, Package string
	Bytes                         []byte
	AST                           *ast.File
	FS                            *token.FileSet
	Imports                       map[string]string
	BadImports                    bool
}
type Decl struct {
	Package, Key, File, Raw, RawSHA, ASTSHA, Header, Suffix string
	Start, End                                              int
	Flat                                                    int64
	Symbols                                                 []string
	Imports                                                 map[string]string
	Closures                                                int
	Generic                                                 bool
	Src                                                     *Source       `json:"-"`
	Fn                                                      *ast.FuncDecl `json:"-"`
}
type Group struct {
	Package, Header, Suffix, Target string
	Imports                         map[string]string
	Decls                           []*Decl
}
type Edit struct {
	Lo, Hi int
	Text   string
}
type Inventory struct {
	Functions    map[string]int
	Nonfunctions map[string][]string
	Imports      []string
	Headers      map[string]string
}

func ck(e error) {
	if e != nil {
		panic(e)
	}
}
func sha(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
func recName(x ast.Expr) string {
	switch t := x.(type) {
	case *ast.StarExpr:
		return recName(t.X)
	case *ast.IndexExpr:
		return recName(t.X)
	case *ast.IndexListExpr:
		return recName(t.X)
	case *ast.Ident:
		return t.Name
	}
	panic("unknown receiver")
}
func key(f *ast.FuncDecl) string {
	if f.Recv == nil {
		return f.Name.Name
	}
	return recName(f.Recv.List[0].Type) + "." + f.Name.Name
}
func astsha(n ast.Node) string {
	var b bytes.Buffer
	ck(ast.Fprint(&b, nil, n, func(n string, v reflect.Value) bool {
		return n != "Obj" && n != "Scope" && n != "Unresolved" && v.Type() != reflect.TypeOf(token.Pos(0))
	}))
	return sha(b.Bytes())
}
func sels(n ast.Node) map[string]bool {
	m := map[string]bool{}
	ast.Inspect(n, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if x, ok := s.X.(*ast.Ident); ok && x.Obj == nil {
				m[x.Name] = true
			}
		}
		return true
	})
	return m
}

var sourceRoot string
var packageNames = map[string]string{}
var moduleDirs = map[string]string{}

func setupNames(root string) {
	sourceRoot = root
	if os.Getenv("LAYOUT_MODULE_GRAPH") == "" || os.Getenv("GOMODCACHE") == "" { panic("explicit LAYOUT_MODULE_GRAPH and GOMODCACHE required") }
	graph, e := os.ReadFile(os.Getenv("LAYOUT_MODULE_GRAPH"))
	ck(e)
	for _, line := range strings.Split(string(graph), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		dir := filepath.Join(os.Getenv("GOMODCACHE"), f[0] + "@" + f[1])
		if len(f) >= 4 && f[2] == "=>" {
			dir = f[3]
		}
		moduleDirs[f[0]] = dir
	}
	moduleDirs["github.com/wago-org/wago"] = root
}
func impName(i *ast.ImportSpec) string {
	if i.Name != nil {
		return i.Name.Name
	}
	p, e := strconv.Unquote(i.Path.Value)
	ck(e)
	if p == "C" {
		return "C"
	}
	if n, ok := packageNames[p]; ok {
		return n
	}
	dir := ""
	if !strings.Contains(strings.Split(p, "/")[0], ".") {
		dir = filepath.Join(runtime.GOROOT(), "src", p)
	} else {
		best := ""
		for mod, base := range moduleDirs {
			if (p == mod || strings.HasPrefix(p, mod+"/")) && len(mod) > len(best) {
				best = mod
				dir = filepath.Join(base, strings.TrimPrefix(p, mod))
			}
		}
	}
	if dir == "" {
		panic("unresolved import module " + p)
	}
	files, e := filepath.Glob(filepath.Join(dir, "*.go"))
	ck(e)
	names := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		a, e := parser.ParseFile(token.NewFileSet(), f, nil, parser.PackageClauseOnly)
		ck(e)
		if a.Name.Name != "main" {
			names[a.Name.Name] = true
		}
	}
	if len(names) != 1 {
		panic(fmt.Sprintf("ambiguous import package %s: %v", p, names))
	}
	for n := range names {
		packageNames[p] = n
		return n
	}
	panic("no package")
}

func suffix(name string) string {
	parts := strings.Split(strings.TrimSuffix(name, ".go"), "_")
	n := len(parts)
	if n > 2 && parts[n-2] == "linux" && parts[n-1] == "amd64" {
		return "_linux_amd64"
	}
	if n > 1 && (parts[n-1] == "linux" || parts[n-1] == "amd64") {
		return "_" + parts[n-1]
	}
	return ""
}
func readSource(path, pkg string) *Source {
	b, e := os.ReadFile(path)
	ck(e)
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, path, b, parser.ParseComments)
	ck(e)
	s := &Source{Path: path, Header: string(b[:fs.Position(f.Package).Offset]), Suffix: suffix(filepath.Base(path)), Package: pkg, Bytes: b, AST: f, FS: fs, Imports: map[string]string{}}
	for _, i := range f.Imports {
		n := impName(i)
		if n == "." || i.Path.Value == "\"C\"" {
			s.BadImports = true
		}
		var ib bytes.Buffer
		ck(format.Node(&ib, fs, i))
		s.Imports[n] = ib.String()
	}
	return s
}
func inv(dir string) Inventory {
	i := Inventory{map[string]int{}, map[string][]string{}, nil, map[string]string{}}
	deps := map[string]bool{}
	files, e := filepath.Glob(filepath.Join(dir, "*.go"))
	ck(e)
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		s := readSource(p, "")
		name := filepath.Base(p)
		i.Headers[name] = sha([]byte(s.Header))
		for _, x := range s.AST.Imports {
			deps[x.Path.Value] = true
		}
		for _, d := range s.AST.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok {
				i.Functions[key(fn)+":"+astsha(fn)]++
				if fn.Name.Name != "init" {
					continue
				}
			} else if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
				continue
			}
			i.Nonfunctions[name] = append(i.Nonfunctions[name], astsha(d))
		}
	}
	for d := range deps {
		i.Imports = append(i.Imports, d)
	}
	sort.Strings(i.Imports)
	return i
}
func apply(b []byte, es []Edit) []byte {
	sort.Slice(es, func(i, j int) bool { return es[i].Lo > es[j].Lo })
	end := len(b)
	for _, e := range es {
		if e.Hi > end {
			panic("overlapping edit")
		}
		b = append(append(append([]byte{}, b[:e.Lo]...), e.Text...), b[e.Hi:]...)
		end = e.Lo
	}
	return b
}
func compatible(g *Group, d *Decl) bool {
	if g.Package != d.Package || g.Header != d.Header || g.Suffix != d.Suffix {
		return false
	}
	for k, v := range d.Imports {
		if old, ok := g.Imports[k]; ok && old != v {
			return false
		}
	}
	return true
}
func main() {
	if len(os.Args) != 5 {
		panic("ROOT MANIFEST plan|hot|neutral OUTPUT")
	}
	root, manifest, arm, out := os.Args[1], os.Args[2], os.Args[3], os.Args[4]
	if arm != "plan" && arm != "hot" && arm != "neutral" {
		panic(arm)
	}
	mb, e := os.ReadFile(manifest)
	ck(e)
	var m Manifest
	ck(json.Unmarshal(mb, &m))
	if len(m.Rows) != 414 {
		panic("expected frozen414")
	}
	setupNames(root)
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	ck(err)
	if !bytes.Contains(mod, []byte("module github.com/wago-org/wago\n")) {
		panic("wrong source module")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	ck(err)
	ctx := build.Default
	ctx.GOOS = "linux"
	ctx.GOARCH = "amd64"
	ctx.CgoEnabled = true
	ctx.BuildTags = nil
	packages := map[string]string{}
	for _, r := range m.Rows {
		rel := strings.TrimPrefix(r.Package, "github.com/wago-org/wago")
		if !strings.HasPrefix(rel, "/src/") {
			panic("package scope")
		}
		packages[r.Package] = filepath.Join(root, rel)
	}
	pkgNames := []string{}
	for p := range packages {
		pkgNames = append(pkgNames, p)
	}
	sort.Strings(pkgNames)
	sources := map[string]*Source{}
	candidates := map[string][]*Decl{}
	before := map[string]Inventory{}
	for _, pkg := range pkgNames {
		dir := packages[pkg]
		realDir, err := filepath.EvalSymlinks(dir)
		ck(err)
		rel, err := filepath.Rel(realRoot, realDir)
		ck(err)
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			panic("source outside worktree")
		}
		for checkDir := dir; checkDir != root; checkDir = filepath.Dir(checkDir) {
			if _, err := os.Stat(filepath.Join(checkDir, "go.mod")); err == nil {
				panic("nested module")
			}
		}
		before[pkg] = inv(dir)
		files, e := filepath.Glob(filepath.Join(dir, "*.go"))
		ck(e)
		for _, p := range files {
			if strings.HasSuffix(p, "_test.go") {
				continue
			}
			ok, e := ctx.MatchFile(dir, filepath.Base(p))
			ck(e)
			if !ok {
				continue
			}
			realPath, err := filepath.EvalSymlinks(p)
			ck(err)
			rpath, err := filepath.Rel(realRoot, realPath)
			ck(err)
			if rpath == ".." || strings.HasPrefix(rpath, ".."+string(os.PathSeparator)) {
				panic("source file outside worktree")
			}
			s := readSource(p, pkg)
			sources[p] = s
			for _, n := range s.AST.Decls {
				f, ok := n.(*ast.FuncDecl)
				if !ok {
					continue
				}
				lo := s.FS.Position(f.Pos()).Offset
				if f.Doc != nil {
					lo = s.FS.Position(f.Doc.Pos()).Offset
				}
				hi := s.FS.Position(f.End()).Offset
				d := &Decl{Package: pkg, Key: key(f), File: filepath.Base(p), Raw: string(s.Bytes[lo:hi]), Start: lo, End: hi, ASTSHA: astsha(f), Header: s.Header, Suffix: s.Suffix, Imports: map[string]string{}, Src: s, Fn: f}
				d.RawSHA = sha([]byte(d.Raw))
				d.Generic = f.Type.TypeParams != nil
				ast.Inspect(f, func(n ast.Node) bool {
					if _, ok := n.(*ast.FuncLit); ok {
						d.Closures++
					}
					return true
				})
				candidates[pkg+":"+d.Key] = append(candidates[pkg+":"+d.Key], d)
			}
		}
	}
	selected := map[*Decl]bool{}
	excluded := []map[string]string{}
	for _, r := range m.Rows {
		ds := candidates[r.Package+":"+r.Hint]
		reason := ""
		if len(ds) != 1 {
			reason = fmt.Sprintf("eligible declaration matches=%d", len(ds))
		} else if ds[0].Fn.Name.Name == "init" {
			reason = "init never moved"
		} else if ds[0].Fn.Body == nil {
			reason = "bodyless assembly/external stub"
		} else if strings.Contains(string(ds[0].Src.Bytes), "//go:generate") || strings.Contains(string(ds[0].Src.Bytes), "//line ") || strings.Contains(string(ds[0].Src.Bytes), "/*line ") || strings.Contains(string(ds[0].Src.Bytes), "//export ") {
			reason = "file-context generate/line/export directive requires separate proof"
		} else if ds[0].Src.BadImports {
			reason = "dot or C imports require separate proof"
		}
		if reason != "" {
			excluded = append(excluded, map[string]string{"symbol": r.Symbol, "reason": reason})
			continue
		}
		d := ds[0]
		d.Flat += r.Flat
		d.Symbols = append(d.Symbols, r.Symbol)
		selected[d] = true
	}
	decls := []*Decl{}
	for d := range selected {
		sort.Strings(d.Symbols)
		uses := sels(d.Fn)
		for n, s := range d.Src.Imports {
			if uses[n] || (n == "unsafe" && strings.Contains(d.Raw, "//go:linkname")) {
				d.Imports[n] = s
			}
		}
		decls = append(decls, d)
	}
	sort.Slice(decls, func(i, j int) bool {
		a, b := decls[i], decls[j]
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		if a.Flat != b.Flat {
			return a.Flat > b.Flat
		}
		return a.Symbols[0] < b.Symbols[0]
	})
	groups := []*Group{}
	for _, d := range decls {
		var g *Group
		for _, x := range groups {
			if compatible(x, d) {
				g = x
				break
			}
		}
		if g == nil {
			g = &Group{Package: d.Package, Header: d.Header, Suffix: d.Suffix, Imports: map[string]string{}}
			groups = append(groups, g)
		}
		for n, s := range d.Imports {
			g.Imports[n] = s
		}
		g.Decls = append(g.Decls, d)
	}
	if arm == "neutral" {
		for _, g := range groups {
			sort.Slice(g.Decls, func(i, j int) bool {
				a, b := g.Decls[i], g.Decls[j]
				if a.File != b.File {
					return a.File < b.File
				}
				return a.Start < b.Start
			})
		}
		sort.SliceStable(groups, func(i, j int) bool {
			a, b := groups[i], groups[j]
			if a.Package != b.Package {
				return a.Package < b.Package
			}
			if a.Decls[0].File != b.Decls[0].File {
				return a.Decls[0].File < b.Decls[0].File
			}
			return a.Decls[0].Start < b.Decls[0].Start
		})
	}
	for idx, g := range groups {
		prefix := "a00_profile_cluster"
		if arm == "neutral" {
			prefix = "zzz_profile_control"
		}
		g.Target = fmt.Sprintf("%s_%03d%s.go", prefix, idx, g.Suffix)
	}
	receipt := map[string]any{"manifest_sha256": sha(mb), "arm": arm, "source": "b5ab03f9", "selected_symbols": len(m.Rows) - len(excluded), "selected_declarations": len(decls), "excluded": excluded, "groups": groups, "before": before, "policy": "fixed profile outer owners; descending aggregate flat CPU then smallest exact symbol; same-group late control in original path/offset order; no init/vars/stubs; source/debug positions necessarily move"}
	if arm != "plan" {
		edits := map[string][]Edit{}
		for _, d := range decls {
			edits[d.Src.Path] = append(edits[d.Src.Path], Edit{d.Start, d.End, ""})
		}
		for path, es := range edits {
			s := sources[path]
			remaining := apply(s.Bytes, append([]Edit{}, es...))
			used := map[string]bool{}
			for _, n := range s.AST.Decls {
				if fn, ok := n.(*ast.FuncDecl); ok {
					found := false
					for _, d := range decls {
						if d.Fn == fn {
							found = true
							break
						}
					}
					if found {
						continue
					}
				}
				if gd, ok := n.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
					continue
				}
				for n := range sels(n) {
					used[n] = true
				}
			}
			for _, n := range s.AST.Decls {
				gd, ok := n.(*ast.GenDecl)
				if !ok || gd.Tok != token.IMPORT {
					continue
				}
				lo, hi := s.FS.Position(gd.Pos()).Offset, s.FS.Position(gd.End()).Offset
				keep := []ast.Spec{}
				for _, sp := range gd.Specs {
					im := sp.(*ast.ImportSpec)
					name := impName(im)
					if name == "_" || used[name] || (name == "unsafe" && bytes.Contains(remaining, []byte("//go:linkname"))) {
						keep = append(keep, sp)
					}
				}
				if len(keep) == len(gd.Specs) {
					continue
				}
				gd.Specs = keep
				var ib bytes.Buffer
				if len(keep) > 0 {
					ck(format.Node(&ib, s.FS, gd))
				}
				es = append(es, Edit{lo, hi, ib.String()})
			}
			b := apply(s.Bytes, es)
			b, e = format.Source(b)
			ck(e)
			ck(os.WriteFile(path, b, 0644))
		}
		for _, g := range groups {
			var b strings.Builder
			b.WriteString(g.Header + "package " + g.Decls[0].Src.AST.Name.Name + "\n\n")
			names := []string{}
			for n := range g.Imports {
				names = append(names, n)
			}
			sort.Strings(names)
			if len(names) > 0 {
				b.WriteString("import (\n")
				for _, n := range names {
					b.WriteString(g.Imports[n] + "\n")
				}
				b.WriteString(")\n\n")
			}
			for _, d := range g.Decls {
				b.WriteString(d.Raw + "\n\n")
			}
			data, e := format.Source([]byte(b.String()))
			ck(e)
			p := filepath.Join(packages[g.Package], g.Target)
			if _, e = os.Stat(p); !os.IsNotExist(e) {
				panic("target exists")
			}
			ck(os.WriteFile(p, data, 0644))
			news := readSource(p, g.Package)
			if news.Header != g.Header || news.Suffix != g.Suffix {
				panic("newheader mismatch")
			}
			got := map[string]string{}
			for _, n := range news.AST.Decls {
				if f, ok := n.(*ast.FuncDecl); ok {
					lo := news.FS.Position(f.Pos()).Offset
					if f.Doc != nil {
						lo = news.FS.Position(f.Doc.Pos()).Offset
					}
					got[key(f)] = string(data[lo:news.FS.Position(f.End()).Offset])
				}
			}
			for _, d := range g.Decls {
				if got[d.Key] != d.Raw {
					panic("raw decl mismatch " + d.Key)
				}
			}
		}
		after := map[string]Inventory{}
		for _, pkg := range pkgNames {
			a := inv(packages[pkg])
			b := before[pkg]
			if !reflect.DeepEqual(a.Functions, b.Functions) || !reflect.DeepEqual(a.Nonfunctions, b.Nonfunctions) || !reflect.DeepEqual(a.Imports, b.Imports) {
				panic("AST/init/import mismatch " + pkg)
			}
			for n, h := range b.Headers {
				if a.Headers[n] != h {
					panic("original header mismatch " + n)
				}
			}
			after[pkg] = a
		}
		receipt["after"] = after
		receipt["raw_declarations_verified"] = len(decls)
	}
	b, e := json.MarshalIndent(receipt, "", "  ")
	ck(e)
	ck(os.WriteFile(out, b, 0644))
	fmt.Printf("%s: %d symbols -> %d declarations, %d groups, %d excluded\n", arm, len(m.Rows)-len(excluded), len(decls), len(groups), len(excluded))
}
