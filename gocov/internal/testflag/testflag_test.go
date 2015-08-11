// Copyright (c) 2015 The Gocov Authors.
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

package testflag

import (
	"reflect"
	"testing"
)

type test struct {
	input        []string
	packageNames []string
	passToTest   []string
}

var tests = []test{{
	input:        []string{"-v", "./subdir"},
	packageNames: []string{"./subdir"},
	passToTest:   []string{"-v"},
}, {
	input:        []string{"./subdir", "-v"},
	packageNames: []string{"./subdir"},
	passToTest:   []string{"-v"},
}, {
	input:        []string{"./...", "--", "positional", "-v"},
	packageNames: []string{"./..."},
	passToTest:   []string{"--", "positional", "-v"},
}, {
	input:        []string{"--", "positional", "-v"},
	packageNames: []string{},
	passToTest:   []string{"--", "positional", "-v"},
}, {
	input:        []string{"-tags", "a b c", "./..."},
	packageNames: []string{"./..."},
	passToTest:   []string{"-tags", "a b c"},
}, {
	input:        []string{"-tags", "a b c"},
	packageNames: nil,
	passToTest:   []string{"-tags", "a b c"},
}, {
	input:        []string{"-i=true", "-tags=a b c", "x", "y", "zzy", "-x"},
	packageNames: []string{"x", "y", "zzy"},
	passToTest:   []string{"-i=true", "-tags=a b c", "-x"},
}, {
	input:        []string{"-h", "-?", "-help"},
	packageNames: nil,
	passToTest:   []string{"-h", "-?", "-help"},
}, {
	input:        []string{"--v", "--tags=a b c", "pkgname"},
	packageNames: []string{"pkgname"},
	passToTest:   []string{"--v", "--tags=a b c"},
}}

func TestSplit(t *testing.T) {
	for _, test := range tests {
		t.Logf("testing: %q", test.input)
		packageNames, passToTest := Split(test.input)
		if !reflect.DeepEqual(packageNames, test.packageNames) {
			t.Errorf("packageNames mismatch: %q != %q", packageNames, test.packageNames)
		}
		if !reflect.DeepEqual(passToTest, test.passToTest) {
			t.Errorf("passToTest mismatch: %q != %q", passToTest, test.passToTest)
		}
	}
}
