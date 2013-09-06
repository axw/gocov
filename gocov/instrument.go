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
}

func (v *stmtVisitor) VisitStmt(s ast.Stmt) {
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
			stmtObj := v.functions[len(v.functions)-1].RegisterStatement(start.Offset, end.Offset)
			expr := makeCall(fmt.Sprint(stmtObj, ".At"))
			stmt := &ast.ExprStmt{X: expr}
			item := []ast.Stmt{stmt}
			*statements = append((*statements)[:i], append(item, (*statements)[i:]...)...)
			i++
		}
		v.VisitStmt(s)
	}
}

type funcVisitor struct {
	*state
}

func (v *funcVisitor) Visit(n ast.Node) ast.Visitor {
	var body *ast.BlockStmt

	switch n := n.(type) {
	case *ast.FuncDecl:
		// Function name is prepended with "T." if there is a receiver, where
		// T is the type of the receiver, dereferenced if it is a pointer.
		name := n.Name.Name
		if n.Recv != nil {
			field := n.Recv.List[0]
			switch recv := field.Type.(type) {
			case *ast.StarExpr:
				name = recv.X.(*ast.Ident).Name + "." + name
			case *ast.Ident:
				name = recv.Name + "." + name
			}
		}
		start, end := v.fset.Position(n.Pos()), v.fset.Position(n.End())
		f := v.pkg.RegisterFunction(name, start.Filename, start.Offset, end.Offset)
		v.state.functions = append(v.state.functions, f)
		body = n.Body

	case *ast.FuncLit:
		// Function literals defined within a function do not get a separate
		// *gocov.Function, rather their statements are counted in the
		// enclosing function.
		//
		// Function literals at the package scope are named "@<Position>",
		// where "<Position>" is the position of the beginning of the function
		// literal.
		start, end := v.fset.Position(n.Pos()), v.fset.Position(n.End())
		var enclosing *gocov.Function
		if len(v.functions) > 0 {
			lastfunc := v.functions[len(v.functions)-1]
			if start.Offset < lastfunc.End {
				enclosing = lastfunc
			}
		}
		if enclosing == nil {
			name := fmt.Sprintf("@%d:%d", start.Line, start.Column)
			f := v.pkg.RegisterFunction(name, start.Filename, start.Offset, end.Offset)
			v.state.functions = append(v.state.functions, f)
		}
		body = n.Body
	}

	if body != nil {
		// TODO function coverage (insert "function.Enter", "function.Leave").
		//
		// FIXME instrumentation no longer records statements in line order,
		// as function literals are processed after the body of a function.
		sv := &stmtVisitor{v.state}
		sv.VisitStmt(body)
	}

	return v
}

func (in *instrumenter) redirectImports(f *ast.File) {
	for _, importSpec := range f.Imports {
		path, _ := strconv.Unquote(importSpec.Path.Value)
		if _, ok := in.instrumented[path]; ok {
			path = instrumentedPackagePath(path)
			importSpec.Path.Value = strconv.Quote(path)
		}
	}
}

func (in *instrumenter) instrumentFile(f *ast.File, fset *token.FileSet, pkgpath string) error {
	pkgObj := in.instrumented[pkgpath]
	pkgCreated := false
	if pkgObj == nil {
		pkgCreated = true
		pkgObj = gocov.RegisterPackage(pkgpath)
		in.instrumented[pkgpath] = pkgObj
	}
	state := &state{fset, f, pkgObj, nil}
	ast.Walk(&funcVisitor{state: state}, f)

	// Count the number of import GenDecl's. They're always first.
	nImportDecls := 0
	for _, decl := range f.Decls {
		if decl, ok := decl.(*ast.GenDecl); !ok || decl.Tok != token.IMPORT {
			break
		}
		nImportDecls++
	}

	// Redirect imports of instrumented packages.
	in.redirectImports(f)

	// Insert variable declarations for registered objects.
	var vardecls []ast.Decl
	pkgvarname := fmt.Sprint(pkgObj)
	if pkgCreated {
		value := makeCall("_gocov.RegisterPackage", makeLit(pkgpath))
		vardecls = append(vardecls, makeVarDecl(pkgvarname, value))
	}
	for _, fn := range state.functions {
		fnvarname := fmt.Sprint(fn)
		value := makeCall(pkgvarname+".RegisterFunction",
			makeLit(fn.Name), makeLit(fn.File),
			makeLit(fn.Start), makeLit(fn.End))
		vardecls = append(vardecls, makeVarDecl(fnvarname, value))
		for _, stmt := range fn.Statements {
			varname := fmt.Sprint(stmt)
			value := makeCall(
				fnvarname+".RegisterStatement",
				makeLit(stmt.Start), makeLit(stmt.End))
			vardecls = append(vardecls, makeVarDecl(varname, value))
		}
	}
	if len(f.Decls) > 0 {
		vardecls = append(vardecls, f.Decls[nImportDecls:]...)
		f.Decls = append(f.Decls[:nImportDecls], vardecls...)
	} else {
		f.Decls = vardecls
	}

	// Add a "gocov" import.
	if pkgCreated {
		gocovImportSpec := &ast.ImportSpec{
			Path: makeLit(gocovPackagePath).(*ast.BasicLit), Name: ast.NewIdent("_gocov")}
		gocovImportGenDecl := &ast.GenDecl{
			Tok: token.IMPORT, Specs: []ast.Spec{gocovImportSpec}}
		tail := make([]ast.Decl, len(f.Decls)-nImportDecls)
		copy(tail, f.Decls[nImportDecls:])
		head := append(f.Decls[:nImportDecls], gocovImportGenDecl)
		f.Decls = append(head, tail...)
	}

	// Clear out all cached comments. This forces the AST printer to use
	// node comments instead, repositioning them correctly.
	f.Comments = nil

	return nil
}
