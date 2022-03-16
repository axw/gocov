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

package main

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/axw/gocov"
	"github.com/axw/gocov/gocovutil"
	"golang.org/x/tools/cover"
)

type packagesCache map[string]*build.Package

func convertProfiles(filenames ...string) error {
	var (
		ps       gocovutil.Packages
		packages = make(packagesCache)
	)

	for i := range filenames {
		converter := converter{
			packages: make(map[string]*gocov.Package),
		}
		profiles, err := cover.ParseProfiles(filenames[i])
		if err != nil {
			return err
		}
		for _, p := range profiles {
			if err := converter.convertProfile(packages, p); err != nil {
				return err
			}
		}

		for _, pkg := range converter.packages {
			ps.AddPackage(pkg)
		}
	}
	if err := marshalJson(os.Stdout, ps); err != nil {
		return err
	}
	return nil
}

type converter struct {
	packages map[string]*gocov.Package
}

// wrapper for gocov.Statement
type statement struct {
	*gocov.Statement
	*StmtExtent
}

func (c *converter) convertProfile(packages packagesCache, p *cover.Profile) error {
	file, pkgpath, err := findFile(packages, p.FileName)
	if err != nil {
		return err
	}
	pkg := c.packages[pkgpath]
	if pkg == nil {
		pkg = &gocov.Package{Name: pkgpath}
		c.packages[pkgpath] = pkg
	}
	// Find function and statement extents; create corresponding
	// gocov.Functions and gocov.Statements, and keep a separate
	// slice of gocov.Statements so we can match them with profile
	// blocks.
	extents, err := findFuncs(file)
	if err != nil {
		return err
	}
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
		pkg.Functions = append(pkg.Functions, f)
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
func findFile(packages packagesCache, file string) (filename, pkgpath string, err error) {
	dir, file := filepath.Split(file)
	if dir != "" {
		dir = strings.TrimSuffix(dir, "/")
	}
	pkg, ok := packages[dir]
	if !ok {
		pkg, err = build.Import(dir, ".", build.FindOnly)
		if err != nil {
			return "", "", fmt.Errorf("can't find %q: %w", file, err)
		}
		packages[dir] = pkg
	}

	return filepath.Join(pkg.Dir, file), pkg.ImportPath, nil
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
