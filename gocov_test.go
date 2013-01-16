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

package gocov

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

func TestMallocs(t *testing.T) {
	ctx := &Context{}
	p := ctx.RegisterPackage("p1")
	f := p.RegisterFunction("f1", "file.go", 0, 1)
	s := f.RegisterStatement(0, 1)

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	m0 := ms.Mallocs

	f.Enter()
	s.At()
	f.Leave()

	runtime.ReadMemStats(&ms)
	mallocs := ms.Mallocs - m0
	if mallocs > 0 {
		t.Errorf("%d mallocs; want 0", mallocs)
	}
}

func TestTraceOutput(t *testing.T) {
	var buf bytes.Buffer
	ctx := &Context{Tracer: &buf}
	check := func(expected string) {
		actual := strings.TrimSpace(buf.String())
		if actual != expected {
			t.Errorf("Expected %q, found %q", expected, actual)
		}
	}

	p := ctx.RegisterPackage("p1")
	check("RegisterPackage(`p1`): gocovObject0")
	buf.Reset()

	f := p.RegisterFunction("f1", "file.go", 0, 1)
	check("gocovObject0.RegisterFunction(`f1`, `file.go`, 0, 1): gocovObject1")
	buf.Reset()

	s := f.RegisterStatement(0, 1)
	check(`gocovObject1.RegisterStatement(0, 1): gocovObject2`)
	buf.Reset()

	f.Enter()
	check(`gocovObject1.Enter()`)
	buf.Reset()

	s.At()
	check(`gocovObject2.At()`)
	buf.Reset()

	f.Leave()
	check(`gocovObject1.Leave()`)
	buf.Reset()
}

func TestTraceFlags(t *testing.T) {
	var buf bytes.Buffer
	ctx := &Context{Tracer: &buf}
	check := func(expected string) {
		actual := strings.TrimSpace(buf.String())
		if actual != expected {
			t.Errorf("Expected %q, found %q", expected, actual)
		}
	}

	p := ctx.RegisterPackage("p1")
	f := p.RegisterFunction("f1", "file.go", 0, 1)
	f.Enter()
	buf.Reset()

	// TraceAll is not set, second entry should be silent.
	f.Enter()
	check("")

	// TraceAll set now, so should get another log message.
	ctx.TraceFlags = TraceAll
	f.Enter()
	check(f.String() + ".Enter()")
	if f.Entered != 3 {
		t.Errorf("Expected f.Entered == 3, found %d", f.Entered)
	}
}

func TestAccumulatePackage(t *testing.T) {
	ctx := &Context{}
	p1_1 := ctx.RegisterPackage("p1")
	p1_2 := ctx.RegisterPackage("p1")
	p2 := ctx.RegisterPackage("p2")
	p3 := ctx.RegisterPackage("p1")
	p3.RegisterFunction("f", "file.go", 0, 1)
	p4 := ctx.RegisterPackage("p1")
	p4.RegisterFunction("f", "file.go", 1, 2)

	var tests = [...]struct {
		a, b       *Package
		expectPass bool
	}{
		// Should work: everything is the same.
		{p1_1, p1_2, true},
		// Should fail: name is different.
		{p1_1, p2, false},
		// Should fail: numbers of functions are different.
		{p1_1, p3, false},
		// Should fail: functions are different.
		{p3, p4, false},
	}

	for _, test := range tests {
		err := test.a.Accumulate(test.b)
		if test.expectPass {
			if err != nil {
				t.Error(err)
			}
		} else {
			if err == nil {
				t.Error("Expected an error")
			}
		}
	}
}

func TestAccumulateFunction(t *testing.T) {
	ctx := &Context{}
	p := ctx.RegisterPackage("p1")
	f1_1 := p.RegisterFunction("f1", "file.go", 0, 1)
	f1_2 := p.RegisterFunction("f1", "file.go", 0, 1)
	f2 := p.RegisterFunction("f2", "file.go", 0, 1)
	f3 := p.RegisterFunction("f1", "file2.go", 0, 1)
	f4 := p.RegisterFunction("f1", "file.go", 2, 3)
	f5 := p.RegisterFunction("f1", "file.go", 0, 1)
	f5.RegisterStatement(0, 1)
	f6 := p.RegisterFunction("f1", "file.go", 0, 1)
	f6.RegisterStatement(2, 3)

	var tests = [...]struct {
		a, b       *Function
		expectPass bool
	}{
		// Should work: everything is the same.
		{f1_1, f1_2, true},
		// Should fail: names are different.
		{f1_1, f2, false},
		// Should fail: files are different.
		{f1_1, f3, false},
		// Should fail: ranges are different.
		{f1_1, f4, false},
		// Should fail: numbers of statements are different.
		{f1_1, f5, false},
		// Should fail: all the same, except statement values.
		{f5, f6, false},
	}

	for _, test := range tests {
		err := test.a.Accumulate(test.b)
		if test.expectPass {
			if err != nil {
				t.Error(err)
			}
		} else {
			if err == nil {
				t.Error("Expected an error")
			}
		}
	}
}

func TestAccumulateStatement(t *testing.T) {
	ctx := &Context{}
	p := ctx.RegisterPackage("p1")
	f := p.RegisterFunction("f1", "file.go", 0, 1)
	s1_1 := f.RegisterStatement(0, 1)
	s1_2 := f.RegisterStatement(0, 1)
	s2 := f.RegisterStatement(2, 3)

	// Should work: ranges are the same.
	if err := s1_1.Accumulate(s1_2); err != nil {
		t.Error(err)
	}

	// Should fail: ranges are not the same.
	if err := s1_1.Accumulate(s2); err == nil {
		t.Errorf("Expected an error")
	}
}

func BenchmarkEnterLeave(b *testing.B) {
	ctx := &Context{}
	p := ctx.RegisterPackage("p1")
	f := p.RegisterFunction("f1", "file.go", 0, 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Enter()
		f.Leave()
	}
}

func BenchmarkAt(b *testing.B) {
	ctx := &Context{}
	p := ctx.RegisterPackage("p1")
	f := p.RegisterFunction("f1", "file.go", 0, 1)
	s := f.RegisterStatement(0, 1)
	f.Enter()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.At()
	}
}
