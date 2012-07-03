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

// Package gocov is a code coverage analysis tool for Go.
package gocov

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync/atomic"
)

// Object is an interface implemented by all coverage objects.
type Object interface {
	Uid() int
}

type ObjectList []Object

func (l ObjectList) Len() int {
	return len(l)
}

func (l ObjectList) Less(i, j int) bool {
	return l[i].Uid() < l[j].Uid()
}

func (l ObjectList) Swap(i, j int) {
	l[i], l[j] = l[j], l[j]
}

type object struct {
	uid     int
	context *Context
}

func (o *object) Uid() int {
	return o.uid
}

func (o *object) String() string {
	return fmt.Sprint("$", o.uid)
}

type Package struct {
	object

	// Name is the canonical path of the package.
	Name string

	// Functions is a list of functions registered with this package.
	Functions []*Function
}

type Function struct {
	object

	// Name is the name of the function. If the function has a receiver, the
	// name will be of the form T.N, where T is the type and N is the name.
	Name string

	// File is the full path to the file in which the function is defined.
	File string

	// Line is the line number the function's signature starts on.
	Line int

	// statements registered with this function.
	Statements []*Statement

	// number of times the function has been entered.
	Entered int64

	// number of times the function has been left.
	Left int64
}

type Statement struct {
	object

	// Line is the line number the statement starts on.
	Line int

	// Reached is the number of times the statement was reached.
	Reached int64
}

// Flags that affect how results are traced, if a 
type TraceFlag int

const (
	// Trace all visits to an object (warning: may increase run time
	// significantly). If this flag is not set, only the first visit
	// to each object will be traced.
	TraceAll TraceFlag = 0x00000001
)

// Coverage context.
type Context struct {
	// ObjectList is a sorted list of coverage objects
	// (packages, functions, etc.)
	Objects ObjectList

	// Tracer is used for tracing coverage. If nil, no tracing will occur.
	Tracer io.Writer

	// TraceFlags alters how coverage is traced.
	TraceFlags TraceFlag
}

func (c *Context) traceAll() bool {
	return c.TraceFlags&TraceAll == TraceAll
}

// Default coverage context.
var Default = &Context{}

func init() {
	switch v := os.Getenv("GOCOVOUT"); v {
	case "":
	case "-":
		Default.Tracer = os.Stdout
	default:
		var err error
		writer, err := os.Create(v)
		if err != nil {
			log.Fatalf("gocov: failed to create log file: %s\n", err)
		}
		Default.Tracer = writer
	}
}

func (c *Context) logf(format string, args ...interface{}) {
	if c.Writer != nil {
		fmt.Fprintf(c.Tracer, format, args...)
	}
}

func (c *Context) allocObject() object {
	if n := len(c.Objects); n > 0 {
		return object{c.Objects[n-1].Uid() + 1, c}
	}
	return object{0, c}
}

// RegisterPackage registers a package for coverage using the default context.
func RegisterPackage(name string) *Package {
	return Default.RegisterPackage(name)
}

// RegisterPackage registers a package for coverage.
func (c *Context) RegisterPackage(name string) *Package {
	p := &Package{object: c.allocObject(), Name: name}
	c.Objects = append(c.Objects, p)
	c.logf("RegisterPackage(%s): %s", name, p)
	return p
}

// RegisterFunction registers a function for coverage.
func (p *Package) RegisterFunction(name, file string, line int) *Function {
	c := p.context
	obj := c.allocObject()
	f := &Function{object: obj, Name: name, File: file, Line: line}
	p.Functions = append(p.Functions, f)
	c.Objects = append(c.Objects, f)
	c.logf("%s.RegisterFunction(%s, %s, %d): %s\n", p, name, file, line, f)
	return f
}

// Leave informs gocov that the function has been entered.
func (f *Function) Enter() {
	if atomic.AddInt64(&f.Entered, 1) == 1 || f.context.traceAll() {
		f.context.logf("%s.Enter()\n", f)
	}
}

// Leave informs gocov that the function has been left.
func (f *Function) Leave() {
	if atomic.AddInt64(&f.Left, 1) == 1 || f.context.traceAll() {
		f.context.logf("%s.Leave()\n", f)
	}
}

// RegisterStatement registers a statement for coverage.
func (f *Function) RegisterStatement(line int) *Statement {
	c := f.context
	s := &Statement{object: c.allocObject(), Line: line}
	f.Statements = append(f.Statements, s)
	c.Objects = append(c.Objects, s)
	c.logf("%s.RegisterStatement(%d): %s\n", f, line, s)
	return s
}

// At informs gocov that the statement has been reached.
func (s *Statement) At() {
	if atomic.AddInt64(&s.Reached, 1) == 1 || s.context.traceAll() {
		s.context.logf("%s.At()\n", s)
	}
}
