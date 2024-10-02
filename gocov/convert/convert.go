// Copyright (c) 2013 The Gocov Authors.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS
// IN THE SOFTWARE.

package convert

import (
	"bytes"
	"fmt"
	"github.com/axw/gocov"
	"github.com/axw/gocov/gocovutil"
	json "github.com/json-iterator/go"
	"go/ast"
	"go/parser"
	"go/token"
	modfile "golang.org/x/mod/modfile"
	"golang.org/x/sync/errgroup"
	"golang.org/x/tools/cover"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

const (
	goModFilename = "go.mod"
)

func marshalJson(w io.Writer, packages []*gocov.Package) error {
	return json.NewEncoder(w).Encode(struct{ Packages []*gocov.Package }{packages})
}

func ConvertProfiles(filenames ...string) ([]byte, error) {
	var (
		ps gocovutil.Packages
	)

	goModContent, err := os.ReadFile(goModFilename)
	if err != nil {
		return nil, fmt.Errorf("getting module name: read go.mod: %w", err)
	}
	moduleName := modfile.ModulePath(goModContent)

	for i := range filenames {
		converter := converter{
			packages: make(map[string]*gocov.Package),
			mu:       &sync.RWMutex{},
		}
		profiles, err := cover.ParseProfiles(filenames[i])
		if err != nil {
			return nil, err
		}

		processedProfiles := make(map[string]interface{})
		processedDirs := make(map[string]interface{})
		mu := &sync.Mutex{}
		funcsChan := make(chan func() error)
		errChan := make(chan error)
		var workerErr error
		go func() {
			for _, p := range profiles {
				p := p
				relativeFilepath := strings.TrimPrefix(strings.TrimPrefix(p.FileName, moduleName), "/")
				absFilepath, err := filepath.Abs(relativeFilepath)
				if err != nil {
					workerErr = fmt.Errorf("getting absolute path of file %q: %w", absFilepath, err)
					break
				}
				_, ok := processedProfiles[absFilepath]
				if !ok {
					processedProfiles[absFilepath] = nil
					funcsChan <- func() error {
						if err := converter.fillFindFuncs(mu, absFilepath); err != nil {
							return fmt.Errorf("fillFindFuncs: %w", err)
						}
						return nil
					}
				}
				dir, _ := filepath.Split(p.FileName)
				_, ok = processedDirs[dir]
				if !ok {
					processedDirs[dir] = nil
					funcsChan <- func() error {
						if err := converter.fillAllPackages(mu, p, moduleName); err != nil {
							return fmt.Errorf("failed to fill all packages: %w", err)
						}
						return nil
					}
				}
			}
			close(funcsChan)
		}()

		wg := &sync.WaitGroup{}
		for idx := 0; idx < 10; idx++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for f := range funcsChan {
					errChan <- f()
				}
			}()
		}

		go func() {
			wg.Wait()
			close(errChan)
		}()

		for err := range errChan {
			if err != nil {
				return nil, fmt.Errorf("failed to process: %w", err)
			}
		}

		if workerErr != nil {
			return nil, fmt.Errorf("generating report: %w", err)
		}

		errGr := errgroup.Group{}
		for _, p := range profiles {
			p := p
			errGr.Go(func() error {
				if err := converter.convertProfile(p, moduleName); err != nil {
					return fmt.Errorf("failed to process concurrently: %w", err)
				}
				return nil
			})
		}
		if err := errGr.Wait(); err != nil {
			return nil, fmt.Errorf("convert profiles: %w", err)
		}

		for _, pkg := range converter.packages {
			ps.AddPackage(pkg)
		}
	}
	buf := bytes.Buffer{}
	if err := marshalJson(&buf, ps); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type converter struct {
	packages map[string]*gocov.Package
	mu       *sync.RWMutex
}

// wrapper for gocov.Statement
type statement struct {
	*gocov.Statement
	*StmtExtent
}

var cacheFindFuncs = make(map[string][]*FuncExtent)

func (c *converter) fillAllPackages(mu *sync.Mutex, p *cover.Profile, moduleName string) error {
	_, pkgpath := findFile(moduleName, p.FileName)

	mu.Lock()
	pkg := c.packages[pkgpath]
	if pkg == nil {
		pkg = &gocov.Package{Name: pkgpath, Mu: &sync.Mutex{}}
		c.packages[pkgpath] = pkg
	}
	mu.Unlock()
	return nil
}

func (c *converter) fillFindFuncs(mu *sync.Mutex, filename string) error {
	extents, err := findFuncs(filename)
	if err != nil {
		return err
	}

	mu.Lock()
	cacheFindFuncs[filename] = extents
	mu.Unlock()
	return nil
}

func (c *converter) convertProfile(p *cover.Profile, moduleName string) error {
	file, pkgpath := findFile(moduleName, p.FileName)
	pkg, ok := c.packages[pkgpath]
	if !ok {
		return fmt.Errorf("not found package: %s", pkgpath)
	}
	// Find function and statement extents; create corresponding
	// gocov.Functions and gocov.Statements, and keep a separate
	// slice of gocov.Statements so we can match them with profile
	// blocks.
	extents := cacheFindFuncs[file]
	var stmts []statement
	for _, fe := range extents {
		f := &gocov.Function{
			Name:  fe.name,
			File:  file,
			Start: fe.startOffset,
			End:   fe.endOffset,
		}
		for _, se := range fe.stmts {
			s := statement{
				Statement:  &gocov.Statement{Start: se.startOffset, End: se.endOffset},
				StmtExtent: se,
			}
			f.Statements = append(f.Statements, s.Statement)
			stmts = append(stmts, s)
		}
		pkg.Mu.Lock()
		pkg.Functions = append(pkg.Functions, f)
		pkg.Mu.Unlock()
	}
	// For each profile block in the file, find the statement(s) it
	// covers and increment the Reached field(s).
	blocks := p.Blocks
	for _, s := range stmts {
		for i, b := range blocks {
			if b.StartLine > s.endLine || (b.StartLine == s.endLine && b.StartCol >= s.endCol) {
				// Past the end of the statement
				blocks = blocks[i:]
				break
			}
			if b.EndLine < s.startLine || (b.EndLine == s.startLine && b.EndCol <= s.startCol) {
				// Before the beginning of the statement
				continue
			}
			s.Reached += int64(b.Count)
			break
		}
	}
	return nil
}

// findFile finds the location of the named file in GOROOT, GOPATH etc.
// moduleName - gitlab.ozon.ru/re/das/api
// importPath - gitlab.ozon.ru/re/das/api/internal/clients
// packageFileName - gitlab.ozon.ru/re/das/api/internal/clients/redis.go
// returns
// absolute path to redis.go on machine and gitlab.ozon.ru/re/das/api/internal/clients
func findFile(moduleName, packageFileName string) (string, string) {
	importPath, _ := path.Split(packageFileName)
	absPath, _ := filepath.Abs(strings.TrimPrefix(strings.TrimPrefix(packageFileName, moduleName), "/"))
	return absPath, strings.TrimSuffix(importPath, "/")
}

// findFuncs parses the file and returns a slice of FuncExtent descriptors.
func findFuncs(name string) ([]*FuncExtent, error) {
	fset := token.NewFileSet()
	parsedFile, err := parser.ParseFile(fset, name, nil, 0)
	if err != nil {
		return nil, err
	}
	visitor := &FuncVisitor{fset: fset}
	ast.Walk(visitor, parsedFile)
	return visitor.funcs, nil
}

type extent struct {
	startOffset int
	startLine   int
	startCol    int
	endOffset   int
	endLine     int
	endCol      int
}

// FuncExtent describes a function's extent in the source by file and position.
type FuncExtent struct {
	extent
	name  string
	stmts []*StmtExtent
}

// StmtExtent describes a statements's extent in the source by file and position.
type StmtExtent extent

// FuncVisitor implements the visitor that builds the function position list for a file.
type FuncVisitor struct {
	fset  *token.FileSet
	funcs []*FuncExtent
}

func functionName(f *ast.FuncDecl) string {
	name := f.Name.Name
	if f.Recv == nil {
		return name
	} else {
		// Function name is prepended with "T." if there is a receiver, where
		// T is the type of the receiver, dereferenced if it is a pointer.
		return exprName(f.Recv.List[0].Type) + "." + name
	}
}

func exprName(x ast.Expr) string {
	switch y := x.(type) {
	case *ast.StarExpr:
		return exprName(y.X)
	case *ast.IndexExpr:
		return fmt.Sprintf("%s[%s]", exprName(y.X), exprName(y.Index))
	case *ast.Ident:
		return y.Name
	default:
		return ""
	}
}

// Visit implements the ast.Visitor interface.
func (v *FuncVisitor) Visit(node ast.Node) ast.Visitor {
	var body *ast.BlockStmt
	var name string
	switch n := node.(type) {
	case *ast.FuncLit:
		body = n.Body
	case *ast.FuncDecl:
		body = n.Body
		name = functionName(n)
	}
	if body != nil {
		start := v.fset.Position(node.Pos())
		end := v.fset.Position(node.End())
		if name == "" {
			name = fmt.Sprintf("@%d:%d", start.Line, start.Column)
		}
		fe := &FuncExtent{
			name: name,
			extent: extent{
				startOffset: start.Offset,
				startLine:   start.Line,
				startCol:    start.Column,
				endOffset:   end.Offset,
				endLine:     end.Line,
				endCol:      end.Column,
			},
		}
		v.funcs = append(v.funcs, fe)
		sv := StmtVisitor{fset: v.fset, function: fe}
		sv.VisitStmt(body)
	}
	return v
}

type StmtVisitor struct {
	fset     *token.FileSet
	function *FuncExtent
}

func (v *StmtVisitor) VisitStmt(s ast.Stmt) {
	var statements *[]ast.Stmt
	switch s := s.(type) {
	case *ast.BlockStmt:
		statements = &s.List
	case *ast.CaseClause:
		statements = &s.Body
	case *ast.CommClause:
		statements = &s.Body
	case *ast.ForStmt:
		if s.Init != nil {
			v.VisitStmt(s.Init)
		}
		if s.Post != nil {
			v.VisitStmt(s.Post)
		}
		v.VisitStmt(s.Body)
	case *ast.IfStmt:
		if s.Init != nil {
			v.VisitStmt(s.Init)
		}
		v.VisitStmt(s.Body)
		if s.Else != nil {
			// Code copied from go.tools/cmd/cover, to deal with "if x {} else if y {}"
			const backupToElse = token.Pos(len("else ")) // The AST doesn't remember the else location. We can make an accurate guess.
			switch stmt := s.Else.(type) {
			case *ast.IfStmt:
				block := &ast.BlockStmt{
					Lbrace: stmt.If - backupToElse, // So the covered part looks like it starts at the "else".
					List:   []ast.Stmt{stmt},
					Rbrace: stmt.End(),
				}
				s.Else = block
			case *ast.BlockStmt:
				stmt.Lbrace -= backupToElse // So the block looks like it starts at the "else".
			default:
				panic("unexpected node type in if")
			}
			v.VisitStmt(s.Else)
		}
	case *ast.LabeledStmt:
		v.VisitStmt(s.Stmt)
	case *ast.RangeStmt:
		v.VisitStmt(s.Body)
	case *ast.SelectStmt:
		v.VisitStmt(s.Body)
	case *ast.SwitchStmt:
		if s.Init != nil {
			v.VisitStmt(s.Init)
		}
		v.VisitStmt(s.Body)
	case *ast.TypeSwitchStmt:
		if s.Init != nil {
			v.VisitStmt(s.Init)
		}
		v.VisitStmt(s.Assign)
		v.VisitStmt(s.Body)
	}
	if statements == nil {
		return
	}
	for i := 0; i < len(*statements); i++ {
		s := (*statements)[i]
		switch s.(type) {
		case *ast.CaseClause, *ast.CommClause, *ast.BlockStmt:
			break
		default:
			start, end := v.fset.Position(s.Pos()), v.fset.Position(s.End())
			se := &StmtExtent{
				startOffset: start.Offset,
				startLine:   start.Line,
				startCol:    start.Column,
				endOffset:   end.Offset,
				endLine:     end.Line,
				endCol:      end.Column,
			}
			v.function.stmts = append(v.function.stmts, se)
		}
		v.VisitStmt(s)
	}
}
