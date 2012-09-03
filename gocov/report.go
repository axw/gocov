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
	"flag"
	"fmt"
	"github.com/axw/gocov"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
)

type report struct {
	packages []*gocov.Package
}

type reportFunction struct {
	*gocov.Function
	statementsReached int
}

type reportFunctionList []reportFunction

func (l reportFunctionList) Len() int {
	return len(l)
}

// TODO make sort method configurable?
func (l reportFunctionList) Less(i, j int) bool {
	var left, right float64
	if len(l[i].Statements) > 0 {
		left = float64(l[i].statementsReached) / float64(len(l[i].Statements))
	}
	if len(l[j].Statements) > 0 {
		right = float64(l[j].statementsReached) / float64(len(l[j].Statements))
	}
	if left < right {
		return true
	}
	return left == right && len(l[i].Statements) < len(l[j].Statements)
}

func (l reportFunctionList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

type reverse struct {
	sort.Interface
}

func (r reverse) Less(i, j int) bool {
	return r.Interface.Less(j, i)
}

// NewReport creates a new report.
func newReport() (r *report) {
	r = &report{}
	return
}

// AddPackage adds a package's coverage information to the report.
func (r *report) addPackage(p *gocov.Package) {
	i := sort.Search(len(r.packages), func(i int) bool {
		return r.packages[i].Name >= r.packages[i].Name
	})
	if i < len(r.packages) && r.packages[i].Name == p.Name {
		panic("package already exists: result merging not implemented yet")
	} else {
		head := r.packages[:i]
		tail := append([]*gocov.Package{p}, r.packages[i:]...)
		r.packages = append(head, tail...)
	}
}

// Clear clears the coverage information from the report.
func (r *report) clear() {
	r.packages = nil
}

// PrintReport prints a coverage report to the given writer.
func printReport(w io.Writer, r *report) {
	w = tabwriter.NewWriter(w, 0, 8, 0, '\t', 0)
	printHeader(w, "Gocov Coverage Report")
	//fmt.Fprintln(w, "Package\tFunction\tStatements\t")
	//fmt.Fprintln(w, "-------\t--------\t---------\t")
	for _, pkg := range r.packages {
		printPackage(w, pkg)
		fmt.Fprintln(w)
	}
	printFooter(w)
}

func printPackage(w io.Writer, pkg *gocov.Package) {
	var totalStatements = 0
	var totalReached = 0
	functions := make(reportFunctionList, len(pkg.Functions))
	for i, fn := range pkg.Functions {
		reached := 0
		for _, stmt := range fn.Statements {
			if stmt.Reached > 0 {
				reached++
			}
		}
		functions[i] = reportFunction{fn, reached}
		if html {
			annotateFunctionToFile(fn, pkg)
		}
	}
	sort.Sort(reverse{functions})
	printPackageHeader(w, pkg)
	for _, fn := range functions {
		stmtPercent := funcCoveragePercent(fn)
		if html {
			var fullFunctionName string = pkg.Name + "." + fn.Name
			fmt.Fprintf(w, " <TR><TD>%s/%s</TD><TD class=\"function\"><A HREF=\"%s.html\"> %s</A></TD><TD class=\"percentage\">%.2f%%</TD> <TD class=\"lines\">(%d/%d)</TD></TR>\n",
				pkg.Name, filepath.Base(fn.File), fullFunctionName, fn.Name, stmtPercent,
				fn.statementsReached, len(fn.Statements))
		} else {
			fmt.Fprintf(w, "%s/%s\t %s\t %.2f%% (%d/%d)\n",
				pkg.Name, filepath.Base(fn.File), fn.Name, stmtPercent,
				fn.statementsReached, len(fn.Statements))
		}
		totalStatements = totalStatements + len(fn.Statements)
		totalReached = totalReached + fn.statementsReached
	}
	printPackageFooter(w, totalReached, totalStatements, calculateCoveragePercent(totalReached, totalStatements))
}

func funcCoveragePercent(fn reportFunction) (percent float64) {
	return calculateCoveragePercent(fn.statementsReached, len(fn.Statements))
}

func calculateCoveragePercent(reached int, statements int) (percent float64) {
	percent = 0
	if statements > 0 {
		percent = float64(reached) / float64(statements) * 100
	}
	return
}

func reportCoverage() (rc int) {
	files := make([]*os.File, 0, 1)
	if flag.NArg() > 1 {
		name := flag.Arg(1)
		file, err := os.Open(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open file (%s): %s\n", name, err)
		}
		files = append(files, file)
	} else {
		files = append(files, os.Stdin)
	}
	report := newReport()
	for _, file := range files {
		data, err := ioutil.ReadAll(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read coverage file: %s\n", err)
			return 1
		}
		packages, err := unmarshalJson(data)
		if err != nil {
			fmt.Fprintf(
				os.Stderr, "failed to unmarshal coverage data: %s\n", err)
			return 1
		}
		for _, pkg := range packages {
			report.addPackage(pkg)
		}
		if file != os.Stdin {
			file.Close()
		}
	}
	fmt.Println()
	printReport(os.Stdout, report)
	return 0
}
