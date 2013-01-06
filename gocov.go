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

// NOTE: Any package dependencies of gocov cannot be coverage tested, as they
// must import gocov itself. Do not add dependencies without consideration.
import (
	"sync"
	"sync/atomic"
	"syscall"
)

// Object is an interface implemented by all coverage objects.
type Object interface {
	Uid() int
}

type ObjectList []Object
type object struct {
	uid     int
	context *Context
}

func (o *object) Uid() int {
	return o.uid
}

func (o *object) String() string {
	return "gocovObject" + itoa(o.uid)
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

	// Start is the start offset of the function's signature.
	Start int

	// End is the end offset of the function.
	End int

	// statements registered with this function.
	Statements []*Statement

	// number of times the function has been entered.
	Entered int64

	// number of times the function has been left.
	Left int64

	// preallocated strings for logging in (*Statement).{Enter,Leave}()
	//
	// These are preallocated so as to avoid introducing heap allocations into
	// instrumented code.
	enterString, leaveString []byte
}

type Statement struct {
	object

	// Start is the start offset of the statement.
	Start int

	// End is the end offset of the statement.
	End int

	// Reached is the number of times the statement was reached.
	Reached int64

	// preallocated string for logging in (*Statement).At()
	//
	// These are preallocated so as to avoid introducing heap allocations into
	// instrumented code.
	atString []byte
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
	sync.Mutex

	// ObjectList is a sorted list of coverage objects
	// (packages, functions, etc.)
	Objects ObjectList

	// Tracer is used for tracing coverage. If nil, no tracing will occur.
	Tracer Writer

	// TraceFlags alters how coverage is traced.
	TraceFlags TraceFlag
}

func (c *Context) traceAll() bool {
	return c.TraceFlags&TraceAll == TraceAll
}

// Default coverage context.
var Default = &Context{}

func init() {
	switch path, _ := syscall.Getenv("GOCOVOUT"); path {
	case "":
		// No tracing
	case "-":
		Default.Tracer = fdwriter(syscall.Stdout)
	default:
		mode := syscall.O_WRONLY | syscall.O_CREAT | syscall.O_TRUNC
		fd, err := syscall.Open(path, mode, 0666)
		if err != nil {
			msg := "gocov: failed to create log file: "
			msg += err.Error() + "\n"
			write(fdwriter(syscall.Stderr), []byte(msg))
			syscall.Exit(1)
		}
		Default.Tracer = fdwriter(int(fd))
	}

	// Remove GOCOVOUT from environ, to prevent noise from child processes.
	// TODO Don't do this; append .pid to output filename.
	syscall.Setenv("GOCOVOUT", "")
}

func (c *Context) log(bytes []byte) {
	if c.Tracer != nil {
		c.Lock()
		write(c.Tracer, bytes)
		c.Unlock()
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
	msg := `RegisterPackage("` + name + `"): ` + p.String()
	c.log([]byte(msg + "\n"))
	return p
}

// Accumulate will accumulate the coverage information from the provided
// Package into this Package.
func (p *Package) Accumulate(p2 *Package) error {
	if p.Name != p2.Name {
		name1 := `"` + p.Name + `"`
		name2 := `"` + p2.Name + `"`
		msg := "Names do not match: " + name1 + " != " + name2
		return strerror(msg)
	}
	if len(p.Functions) != len(p2.Functions) {
		n1 := itoa(len(p.Functions))
		n2 := itoa(len(p2.Functions))
		msg := "Function counts do not match: " + n1 + " != " + n2
		return strerror(msg)
	}
	for i, f := range p.Functions {
		err := f.Accumulate(p2.Functions[i])
		if err != nil {
			return err
		}
	}
	return nil
}

// RegisterFunction registers a function for coverage.
func (p *Package) RegisterFunction(name, file string, startOffset, endOffset int) *Function {
	c := p.context
	obj := c.allocObject()
	f := &Function{
		object:      obj,
		Name:        name,
		File:        file,
		Start:       startOffset,
		End:         endOffset,
		enterString: []byte(obj.String() + ".Enter()\n"),
		leaveString: []byte(obj.String() + ".Leave()\n"),
	}
	p.Functions = append(p.Functions, f)
	c.Objects = append(c.Objects, f)
	msg := p.String() + ".RegisterFunction("
	msg += `"` + name + `", "` + file + `", `
	msg += itoa(startOffset) + ", " + itoa(endOffset)
	msg += "): " + f.String()
	c.log([]byte(msg + "\n"))
	return f
}

// Accumulate will accumulate the coverage information from the provided
// Function into this Function.
func (f *Function) Accumulate(f2 *Function) error {
	if f.Name != f2.Name {
		name1 := `"` + f.Name + `"`
		name2 := `"` + f2.Name + `"`
		msg := "Names do not match: " + name1 + " != " + name2
		return strerror(msg)
	}
	if f.File != f2.File {
		file1 := `"` + f.File + `"`
		file2 := `"` + f2.File + `"`
		msg := "Files do not match: " + file1 + " != " + file2
		return strerror(msg)
	}
	if f.Start != f2.Start || f.End != f2.End {
		r1 := itoa(f.Start) + "-" + itoa(f.End)
		r2 := itoa(f2.Start) + "-" + itoa(f2.End)
		msg := "Source ranges do not match: " + r1 + " != " + r2
		return strerror(msg)
	}
	if len(f.Statements) != len(f2.Statements) {
		n1 := itoa(len(f.Statements))
		n2 := itoa(len(f2.Statements))
		msg := "Number of statements do not match: " + n1 + " != " + n2
		return strerror(msg)
	}
	f.Entered += f2.Entered
	f.Left += f2.Left
	for i, s := range f.Statements {
		err := s.Accumulate(f2.Statements[i])
		if err != nil {
			return err
		}
	}
	return nil
}

// Enter informs gocov that the function has been entered.
func (f *Function) Enter() {
	if atomic.AddInt64(&f.Entered, 1) == 1 || f.context.traceAll() {
		f.context.log(f.enterString)
	}
}

// Leave informs gocov that the function has been left.
func (f *Function) Leave() {
	if atomic.AddInt64(&f.Left, 1) == 1 || f.context.traceAll() {
		f.context.log(f.leaveString)
	}
}

// RegisterStatement registers a statement for coverage.
func (f *Function) RegisterStatement(startOffset, endOffset int) *Statement {
	c := f.context
	obj := c.allocObject()
	s := &Statement{
		object:   obj,
		Start:    startOffset,
		End:      endOffset,
		atString: []byte(obj.String() + ".At()\n"),
	}
	f.Statements = append(f.Statements, s)
	c.Objects = append(c.Objects, s)
	msg := f.String() + ".RegisterStatement("
	msg += itoa(startOffset) + ", " + itoa(endOffset)
	msg += "): " + s.String()
	c.log([]byte(msg + "\n"))
	return s
}

// Accumulate will accumulate the coverage information from the provided
// Statement into this Statement.
func (s *Statement) Accumulate(s2 *Statement) error {
	if s.Start != s2.Start || s.End != s2.End {
		r1 := itoa(s.Start) + "-" + itoa(s.End)
		r2 := itoa(s2.Start) + "-" + itoa(s2.End)
		msg := "Source ranges do not match: " + r1 + " != " + r2
		return strerror(msg)
	}
	s.Reached += s2.Reached
	return nil
}

// At informs gocov that the statement has been reached.
func (s *Statement) At() {
	if atomic.AddInt64(&s.Reached, 1) == 1 || s.context.traceAll() {
		s.context.log(s.atString)
	}
}
