// Copyright (c) 2013 The Gocov Authors.
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
	"flag"
	"fmt"
	"testing"
)

const testdata = "github.com/axw/gocov/gocov/testdata"

func cloneFlagSet(f *flag.FlagSet) *flag.FlagSet {
	clone := flag.NewFlagSet("", flag.ContinueOnError)
	f.VisitAll(func(f *flag.Flag) { clone.Var(f.Value, f.Name, f.Usage) })
	return clone
}

func TestTestFlags(t *testing.T) {
	flags := cloneFlagSet(testFlags)
	extraArgs := []string{"pkg1", "pkg2", "-extra"}
	err := flags.Parse(append([]string{"-tags=test"}, extraArgs...))
	if err != nil {
		t.Fatal(err)
	}
	tagsFlag := flags.Lookup("tags")
	if tagsFlag == nil {
		t.Fatalf("'tags' flag not found in FlagSet")
	}
	if value := tagsFlag.Value.String(); value != "test" {
		t.Fatalf("Expected -tags=%q, found %q", "test", value)
	}
	nargs := flags.NArg()
	if nargs != len(extraArgs) {
		t.Errorf("Expected %d args, found %d", len(extraArgs), nargs)
	}
	for i := 0; i < nargs; i++ {
		exp := extraArgs[i]
		act := flags.Arg(i)
		if act != exp {
			t.Errorf("Unexpected arg #d: expected %q, found %q", i+1, exp, act)
		}
	}
}

func checkSlices(t *testing.T, prefix, typ string, expected, actual []string) {
	nexp, nact := len(expected), len(actual)
	if nexp != nact {
		t.Errorf("%s: expected %d %ss (%v), received %d (%v)", prefix, nexp, typ, expected, nact, actual)
	} else {
		for j, exp := range expected {
			act := actual[j]
			if exp != act {
				t.Errorf("%s: Expected %s %q, received %q", prefix, typ, exp, act)
			}
		}
	}
}

func TestPackagesAndTestargs(t *testing.T) {
	type testcase struct {
		args     []string
		packages []string
		testargs []string
	}
	testcases := []testcase{
		{
			[]string{"-tags=tag1", testdata + "/tags"},
			[]string{testdata + "/tags"}, nil,
		},
		{
			[]string{"-tags", "tag1", testdata + "/tags"},
			[]string{testdata + "/tags"}, nil,
		},
		{
			[]string{testdata +"/tags", "-tags", "tag1"},
			[]string{testdata + "/tags"},
            []string{"-tags", "tag1"},
		},
		{
			[]string{testdata +"/tags", "-tags=tag1"},
			[]string{testdata + "/tags"},
            []string{"-tags=tag1"},
		},
	}
	for i, tc := range testcases {
		*testTagsFlag = ""
		prefix := fmt.Sprintf("[Test #%d]", i+1)
		if err := testFlags.Parse(tc.args); err != nil {
			t.Errorf("%s: Failed to parse args %v: %v", prefix, tc.args, err)
			continue
		}
		packages, testargs, err := packagesAndTestargs()
		if err != nil {
			t.Errorf("%s: packagesAndTestargs failed: %v", prefix, err)
			continue
		}
		checkSlices(t, prefix, "package", packages, tc.packages)
		checkSlices(t, prefix, "testarg", testargs, tc.testargs)
	}
}
