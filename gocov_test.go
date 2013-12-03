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

import "testing"

func registerPackage(name string) *Package {
	return &Package{Name: name}
}

func registerFunction(p *Package, name, file string, startOffset, endOffset int) *Function {
	f := &Function{Name: name, File: file, Start: startOffset, End: endOffset}
	p.Functions = append(p.Functions, f)
	return f
}

func registerStatement(f *Function, startOffset, endOffset int) *Statement {
	s := &Statement{Start: startOffset, End: endOffset}
	f.Statements = append(f.Statements, s)
	return s
}

func TestAccumulatePackage(t *testing.T) {
	p1_1 := registerPackage("p1")
	p1_2 := registerPackage("p1")
	p2 := registerPackage("p2")
	p3 := registerPackage("p1")
	registerFunction(p3, "f", "file.go", 0, 1)
	p4 := registerPackage("p1")
	registerFunction(p4, "f", "file.go", 1, 2)

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
	p := registerPackage("p1")
	f1_1 := registerFunction(p, "f1", "file.go", 0, 1)
	f1_2 := registerFunction(p, "f1", "file.go", 0, 1)
	f2 := registerFunction(p, "f2", "file.go", 0, 1)
	f3 := registerFunction(p, "f1", "file2.go", 0, 1)
	f4 := registerFunction(p, "f1", "file.go", 2, 3)
	f5 := registerFunction(p, "f1", "file.go", 0, 1)
	registerStatement(f5, 0, 1)
	f6 := registerFunction(p, "f1", "file.go", 0, 1)
	registerStatement(f6, 2, 3)

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
	p := registerPackage("p1")
	f := registerFunction(p, "f1", "file.go", 0, 1)
	s1_1 := registerStatement(f, 0, 1)
	s1_2 := registerStatement(f, 0, 1)
	s2 := registerStatement(f, 2, 3)

	// Should work: ranges are the same.
	if err := s1_1.Accumulate(s1_2); err != nil {
		t.Error(err)
	}

	// Should fail: ranges are not the same.
	if err := s1_1.Accumulate(s2); err == nil {
		t.Errorf("Expected an error")
	}
}
