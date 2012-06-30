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

package gocov

import (
	"fmt"
	"io"
	"os"
)

var writer io.Writer
var objectCount int

type Object struct {
	Uid int
}

func (o *Object) String() string {
	return fmt.Sprint("$", o.Uid)
}

type Function struct {
	Object
	Name string
	File string
	Line int

	// statements registered with this function.
	Statements []*Statement

	// number of times the function has been entered.
	Entered int

	// number of times the function has been left.
	Left int
}

type Statement struct {
	Object
	Line int

	// number of times the statement was reached.
	Reached int
}

func init() {
	switch out := os.Getenv("GOCOVOUT"); out {
	case "":
		// no output
	case "-":
		writer = os.Stdout
	default:
		file, err := os.Create(out)
		if err != nil {
			writer = file
		} else {
			if file != nil {
				file.Close()
			}
		}
	}
}

func allocUid() int {
	uid := objectCount
	objectCount++
	return uid
}

func logf(format string, args ...interface{}) {
	if writer != nil {
		fmt.Fprintf(writer, format, args...)
	}
}

// RegisterFunction registers a function for coverage, returning a
// new *Function.
func RegisterFunction(name, file string, line int) *Function {
	f := &Function{Object: Object{allocUid()}, Name: name, File: file, Line: line}
	logf("RegisterFunction(%#v, %#v, %d): %s\n", name, file, line, f)
	return f
}

func (f *Function) Enter() {
	logf("%s.Enter()\n", f)
	f.Entered++
}

func (f *Function) Leave() {
	logf("%s.Leave()\n", f)
	f.Left++
}

func (f *Function) RegisterStatement(line int) *Statement {
	s := &Statement{Object: Object{allocUid()}, Line: line}
	f.Statements = append(f.Statements, s)
	logf("%s.RegisterStatement(%d): %s\n", f, line, s)
	return s
}

// At is called each time the statement is reached.
func (s *Statement) At() {
	logf("%s.At()\n", s)
	s.Reached++
}

