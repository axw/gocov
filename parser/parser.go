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

package parser

import (
	"fmt"
	"github.com/axw/gocov"
	"go/scanner"
	"go/token"
	"io/ioutil"
	"os"
	"sort"
	"strconv"
	"strings"
)

const gocovObjectPrefix = "gocovObject"

func errorHandler(pos token.Position, msg string) {
	fmt.Fprintf(os.Stderr, "scanning error: %s [%s]", msg, pos)
}

func objnameToUid(objname string) int {
	if !strings.HasPrefix(objname, gocovObjectPrefix) {
		panic(fmt.Errorf("expected gocov object name, found: %#q", objname))
	}
	val, err := strconv.Atoi(objname[len(gocovObjectPrefix):])
	if err != nil {
		panic(err)
	}
	return val
}

type parser struct {
	*token.FileSet
	*scanner.Scanner
	tok token.Token
	pos token.Pos
	lit string

	context  *gocov.Context
	objects  map[int]gocov.Object
	packages []*gocov.Package
}

func (p *parser) next() token.Token {
	p.pos, p.tok, p.lit = p.Scan()
	return p.tok
}

func (p *parser) expect(tok token.Token) {
	if p.tok != tok {
		panic(fmt.Errorf("expected '%s', found '%s' (%s)",
			tok, p.tok, p.Position(p.pos)))
	}
}

func (p *parser) expectNext(tok token.Token) {
	p.next()
	p.expect(tok)
}

func (p *parser) parseRegisterPackage() {
	p.expectNext(token.LPAREN)
	p.expectNext(token.STRING)
	name, _ := strconv.Unquote(p.lit)
	p.expectNext(token.RPAREN)
	p.expectNext(token.COLON)
	p.expectNext(token.IDENT)
	uid := objnameToUid(p.lit)
	pkg := p.context.RegisterPackage(name)
	if pkg.Uid() != uid {
		panic(fmt.Errorf("uid differs: source must have changed"))
	}
	p.objects[uid] = pkg
	p.packages = append(p.packages, pkg)
}

func (p *parser) parseRegisterFunction(pkg *gocov.Package) {
	p.expectNext(token.LPAREN)
	p.expectNext(token.STRING)
	name, _ := strconv.Unquote(p.lit)
	p.expectNext(token.COMMA)
	p.expectNext(token.STRING)
	file, _ := strconv.Unquote(p.lit)
	p.expectNext(token.COMMA)
	p.expectNext(token.INT)
	startOffset, _ := strconv.Atoi(p.lit)
	p.expectNext(token.COMMA)
	p.expectNext(token.INT)
	endOffset, _ := strconv.Atoi(p.lit)
	p.expectNext(token.RPAREN)
	p.expectNext(token.COLON)
	p.expectNext(token.IDENT)
	uid := objnameToUid(p.lit)
	fn := pkg.RegisterFunction(name, file, startOffset, endOffset)
	if fn.Uid() != uid {
		panic(fmt.Errorf("uid differs: source must have changed"))
	}
	p.objects[uid] = fn
}

func (p *parser) parseRegisterStatement(fn *gocov.Function) {
	p.expectNext(token.LPAREN)
	p.expectNext(token.INT)
	startOffset, _ := strconv.Atoi(p.lit)
	p.expectNext(token.COMMA)
	p.expectNext(token.INT)
	endOffset, _ := strconv.Atoi(p.lit)
	p.expectNext(token.RPAREN)
	p.expectNext(token.COLON)
	p.expectNext(token.IDENT)
	uid := objnameToUid(p.lit)
	stmt := fn.RegisterStatement(startOffset, endOffset)
	if stmt.Uid() != uid {
		panic(fmt.Errorf("uid differs: source must have changed"))
	}
	p.objects[uid] = stmt
}

func (p *parser) parseEnterLeave(fn *gocov.Function, entered bool) {
	p.expectNext(token.LPAREN)
	p.expectNext(token.RPAREN)
	if entered {
		fn.Enter()
	} else {
		fn.Leave()
	}
}

func (p *parser) parseAt(stmt *gocov.Statement) {
	p.expectNext(token.LPAREN)
	p.expectNext(token.RPAREN)
	stmt.At()
}

func (p *parser) parse() {
	for tok := p.next(); tok != token.EOF; tok = p.next() {
		p.expect(token.IDENT)
		if p.lit == "RegisterPackage" {
			p.parseRegisterPackage()
		} else {
			uid := objnameToUid(p.lit)
			obj := p.objects[uid]
			if obj == nil {
				panic(fmt.Errorf("invalid object uid: %v", uid))
			}
			p.expectNext(token.PERIOD)
			p.expectNext(token.IDENT)
			switch p.lit {
			case "RegisterFunction":
				p.parseRegisterFunction(obj.(*gocov.Package))
			case "RegisterStatement":
				p.parseRegisterStatement(obj.(*gocov.Function))
			case "Enter", "Leave":
				p.parseEnterLeave(obj.(*gocov.Function), p.lit == "Enter")
			case "At":
				p.parseAt(obj.(*gocov.Statement))
			}
		}
		p.next()
		p.expect(token.SEMICOLON)
	}
}

func ParseTrace(path string) (pkgs []*gocov.Package, err error) {
	defer func() {
		if e := recover(); e != nil {
			if e, ok := e.(error); ok {
				err = e
				return
			}
			err = fmt.Errorf("%s", e)
		}
	}()

	finfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	src, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	f := fset.AddFile(path, fset.Base(), int(finfo.Size()))
	s := &scanner.Scanner{}
	s.Init(f, src, errorHandler, 0)
	p := &parser{
		FileSet: fset,
		Scanner: s,
		tok:     token.Token(-1),
		objects: make(map[int]gocov.Object),
		context: &gocov.Context{},
	}
	p.parse()

	// Merge packages with the same path. This is to cater for "." imports,
	// which can result in two copies of the same package existing
	// simultaneously within a program.
	for _, p := range p.packages {
		i := sort.Search(len(pkgs), func(i int) bool {
			return pkgs[i].Name >= p.Name
		})
		if i < len(pkgs) && pkgs[i].Name == p.Name {
			pkgs[i].Accumulate(p)
		} else {
			head := pkgs[:i]
			tail := append([]*gocov.Package{p}, pkgs[i:]...)
			pkgs = append(head, tail...)
		}
	}
	return
}
