// Copyright (c) 2012 The Gocov Authors.
// 
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the "Software"), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
// of the Software, and to permit persons to whom the Software is furnished to do
// so, subject to the following conditions:
// 
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
// 
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
	"fmt"
	"github.com/axw/gocov"
	"go/ast"
	"go/token"
	"strconv"
)

type state struct {
	fset      *token.FileSet
	file      *ast.File
	functions []*gocov.Function
}

func (s *state) getFunctionObjectName() string {
	return fmt.Sprint("gocovFunc", len(s.functions)-1)
}

func (s *state) getStatementObjectName() string {
	fn := s.functions[len(s.functions)-1]
	return fmt.Sprint(s.getFunctionObjectName(), "Stmt", len(fn.Statements)-1)
}

func makeIdent(name string) *ast.Ident {
	return &ast.Ident{Name: name}
}

func makeCall(fun string, args ...ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{Fun: makeIdent(fun), Args: args}
}

func makeLit(x interface{}) ast.Expr {
	var kind token.Token
	switch x.(type) {
	case uint8, uint16, uint32, uint64, int8, int16, int32, int64, uint, int:
		kind = token.INT
	case float32, float64:
		kind = token.FLOAT
	case complex64, complex128:
		kind = token.IMAG
	case string:
		kind = token.STRING
	default:
		panic(fmt.Sprintf("unhandled literal type: %T", x))
	}
	return &ast.BasicLit{Kind: kind, Value: fmt.Sprintf("%#v", x)}
}

func makeVarDecl(name string, value ast.Expr) *ast.GenDecl {
	spec := &ast.ValueSpec{Names: []*ast.Ident{makeIdent(name)}, Values: []ast.Expr{value}}
	return &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{spec}}
}

type stmtVisitor struct {
	*state
	block *ast.BlockStmt
}
func (v *stmtVisitor) Visit(n ast.Node) ast.Visitor {
	if b, ok := n.(*ast.BlockStmt); ok {
		for i := 0; i < len(b.List); i += 2 {
			s := b.List[i]
			line := v.fset.Position(s.Pos()).Line
			v.functions[len(v.functions)-1].RegisterStatement(line)
			expr := makeCall(v.getStatementObjectName()+".At")
			stmt := &ast.ExprStmt{expr}
			item := []ast.Stmt{stmt}
			b.List = append(b.List[:i], append(item, b.List[i:]...)...)
		}
		v = &stmtVisitor{v.state, b}
	}
	return v
}

type funcVisitor struct {
	*state
}
func (v *funcVisitor) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case *ast.FuncDecl:
		// TODO function coverage (insert "function.Enter", "function.Leave").
		// TODO format receiver name into registered function name.
		f_ := v.fset.File(n.Pos())
		file, line := f_.Name(), f_.Line(n.Pos())
		f := gocov.RegisterFunction(n.Name.Name, file, line)
		v.state.functions = append(v.state.functions, f)
		return &stmtVisitor{v.state, nil}
	}
	return v
}

func (in *instrumenter) instrumentFile(f *ast.File, fset *token.FileSet) error {
	state := &state{fset, f, nil}
	ast.SortImports(fset, f)
	ast.Walk(&funcVisitor{state}, f)

	// Insert variable declarations for registered objects.
	var vardecls []ast.Decl
	for i, fn := range state.functions {
		fnvarname := fmt.Sprint("gocovFunc", i)
		value := makeCall("gocov.RegisterFunction",
			makeLit(fn.Name), makeLit(fn.File), makeLit(fn.Line))
		vardecls = append(vardecls, makeVarDecl(fnvarname, value))
		for i, stmt := range fn.Statements {
			varname := fmt.Sprint(fnvarname, "Stmt", i)
			value := makeCall(fnvarname + ".RegisterStatement", makeLit(stmt.Line))
			vardecls = append(vardecls, makeVarDecl(varname, value))
		}
	}
	f.Decls = append(f.Decls[:1], append(vardecls, f.Decls[1:]...)...)

	// Add a "gocov" import.
	// TODO check something was actually instrumented by the walker.
	if len(state.functions) > 0 {
		found := false
		for _, importSpec := range f.Imports {
			if importSpec.Path.Value == strconv.Quote(gocovPackagePath) {
				found = true
				break
			}
		}
		if !found {
			gocovImportSpec := &ast.ImportSpec{Path: makeLit(gocovPackagePath).(*ast.BasicLit)}
			gocovImportGenDecl := &ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{gocovImportSpec}}
			f.Decls = append([]ast.Decl{gocovImportGenDecl}, f.Decls...)
		}
	}

	return nil
}

