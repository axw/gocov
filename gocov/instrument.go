// Copyright (c) 2012 The Gocov Authors.
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
	"github.com/axw/gocov"
	"go/ast"
	"go/token"
	"strconv"
)

type state struct {
	fset      *token.FileSet
	file      *ast.File
	pkg       *gocov.Package
	functions []*gocov.Function
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
		for i := 0; i < len(b.List); i++ {
			s := b.List[i]
			if _, caseClause := s.(*ast.CaseClause); !caseClause {
				line := v.fset.Position(s.Pos()).Line
				stmtObj := v.functions[len(v.functions)-1].RegisterStatement(line)
				expr := makeCall(fmt.Sprint(stmtObj, ".At"))
				stmt := &ast.ExprStmt{X: expr}
				item := []ast.Stmt{stmt}
				b.List = append(b.List[:i], append(item, b.List[i:]...)...)
				i++
			}
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
		f := v.pkg.RegisterFunction(n.Name.Name, file, line)
		v.state.functions = append(v.state.functions, f)
		return &stmtVisitor{v.state, nil}
	}
	return v
}

func (in *instrumenter) instrumentFile(f *ast.File, fset *token.FileSet) error {
	pkgObj := in.instrumented[f.Name.Name]
	pkgCreated := false
	if pkgObj == nil {
		pkgCreated = true
		pkgObj = gocov.RegisterPackage(f.Name.Name)
		in.instrumented[f.Name.Name] = pkgObj
	}
	state := &state{fset, f, pkgObj, nil}
	ast.SortImports(fset, f)
	ast.Walk(&funcVisitor{state}, f)

	// Insert variable declarations for registered objects.
	var vardecls []ast.Decl
	var pkgvarname string
	if pkgCreated {
		pkgvarname = fmt.Sprint(pkgObj)
		value := makeCall("gocov.RegisterPackage", makeLit(f.Name.Name))
		vardecls = append(vardecls, makeVarDecl(pkgvarname, value))
	} else {
		pkgvarname = fmt.Sprint(pkgObj)
	}
	for _, fn := range state.functions {
		fnvarname := fmt.Sprint(fn)
		value := makeCall(pkgvarname+".RegisterFunction",
			makeLit(fn.Name), makeLit(fn.File), makeLit(fn.Line))
		vardecls = append(vardecls, makeVarDecl(fnvarname, value))
		for _, stmt := range fn.Statements {
			varname := fmt.Sprint(stmt)
			value := makeCall(
				fnvarname+".RegisterStatement", makeLit(stmt.Line))
			vardecls = append(vardecls, makeVarDecl(varname, value))
		}
	}
	if len(f.Decls) > 0 {
		f.Decls = append(f.Decls[:1], append(vardecls, f.Decls[1:]...)...)
	} else {
		f.Decls = vardecls
	}

	// Add a "gocov" import.
	if pkgCreated && len(pkgObj.Functions) > 0 {
		found := false
		for _, importSpec := range f.Imports {
			if importSpec.Path.Value == strconv.Quote(gocovPackagePath) {
				found = true
				break
			}
		}
		if !found {
			gocovImportSpec := &ast.ImportSpec{
				Path: makeLit(gocovPackagePath).(*ast.BasicLit)}
			gocovImportGenDecl := &ast.GenDecl{
				Tok: token.IMPORT, Specs: []ast.Spec{gocovImportSpec}}
			f.Decls = append([]ast.Decl{gocovImportGenDecl}, f.Decls...)
		}
	}

	return nil
}
