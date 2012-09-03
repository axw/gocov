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
	"go/token"
	"io/ioutil"
	"io"
	"math"
	"os"
	"sort"
	"strings"
)

const (
	hitPrefix  = "    "
	missPrefix = "MISS"
	htmlMissClass = "miss"
	htmlHitClass = "hit"
	htmlFooter = "</BODY></HTML>"
)

type packageList []*gocov.Package
type functionList []*gocov.Function

func (l packageList) Len() int {
	return len(l)
}

func (l packageList) Less(i, j int) bool {
	return l[i].Name < l[j].Name
}

func (l packageList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func (l functionList) Len() int {
	return len(l)
}

func (l functionList) Less(i, j int) bool {
	return l[i].Name < l[j].Name
}

func (l functionList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

type annotator struct {
	fset  *token.FileSet
	files map[string]*token.File
}

func annotateSource() (rc int) {
	if flag.NArg() == 1 {
		fmt.Fprintf(os.Stderr, "missing coverage file and functions\n")
		return 1
	} else if flag.NArg() < 3 {
		fmt.Fprintf(os.Stderr, "missing functions\n")
		return 1
	}

	var data []byte
	var err error
	if filename := flag.Arg(1); filename == "-" {
		data, err = ioutil.ReadAll(os.Stdin)
	} else {
		data, err = ioutil.ReadFile(filename)
	}
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

	// Sort packages, functions by name.
	sort.Sort(packageList(packages))
	for _, pkg := range packages {
		sort.Sort(functionList(pkg.Functions))
	}

	a := &annotator{}
	a.fset = token.NewFileSet()
	a.files = make(map[string]*token.File)
	for i := 2; i < flag.NArg(); i++ {
		funcName := flag.Arg(i)
		dotIndex := strings.Index(funcName, ".")
		if dotIndex == -1 {
			// TODO maybe check if there's just one matching package?
			fmt.Fprintf(os.Stderr, "warning: unqualified function '%s', skipping\n", funcName)
			continue
		}

		pkgName := funcName[:dotIndex]
		funcName = funcName[dotIndex+1:]
		i := sort.Search(len(packages), func(i int) bool {
			return packages[i].Name >= pkgName
		})
		if i < len(packages) && packages[i].Name == pkgName {
			pkg := packages[i]
			i := sort.Search(len(pkg.Functions), func(i int) bool {
				return pkg.Functions[i].Name >= funcName
			})
			if i < len(pkg.Functions) && pkg.Functions[i].Name == funcName {
				fn := pkg.Functions[i]
				err := a.printFunctionSource(os.Stdout, fn)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to annotate function '%s.%s'\n",
						pkgName, funcName)
				}
			} else {
				fmt.Fprintf(os.Stderr,
					"warning: no coverage data for function '%s.%s', skipping\n",
					pkgName, funcName)
			}
		} else {
			fmt.Fprintf(os.Stderr,
				"warning: no coverage data for package '%s', skipping\n", pkgName)
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "annotation failed for '%s': %s\n", funcName, err)
			return 1
		}
	}
	return
}

// NOTE Non-ideal as it creates still a new annotator for each run
func annotateFunctionToFile(fn *gocov.Function, pkg *gocov.Package){
	a := &annotator{}
	a.fset = token.NewFileSet()
	a.files = make(map[string]*token.File)
	var fullFunctionName string = pkg.Name + "." + fn.Name
	f, err := os.OpenFile(fullFunctionName + ".html", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666) 
	if err != nil {
		return 
	}  
    defer f.Close()
	error := a.printFunctionSource(f, fn)
	if error != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to annotate function '%s.'\n", fn.Name)
	}
}

func (a *annotator) printFunctionSource(w io.Writer, fn *gocov.Function) error {
	// Load the file for line information. Probably overkill, maybe
	// just compute the lines from offsets in here.
	setContent := false
	file := a.files[fn.File]
	if file == nil {
		info, err := os.Stat(fn.File)
		if err != nil {
			return err
		}
		file = a.fset.AddFile(fn.File, a.fset.Base(), int(info.Size()))
		setContent = true
	}

	data, err := ioutil.ReadFile(fn.File)
	if err != nil {
		return err
	}
	if setContent {
		// This processes the content and records line number info.
		file.SetLinesForContent(data)
	}

	statements := fn.Statements[:]
	lineno := file.Line(file.Pos(fn.Start))
	lines := strings.Split(string(data)[fn.Start:fn.End], "\n")
	linenoWidth := int(math.Log10(float64(lineno+len(lines)))) + 1
	if html {
		printHeader(w, "Gocov coverage for " + fn.Name)
		fmt.Fprintln(w, "<PRE>")
	}
	fmt.Fprintln(w)
	for i, line := range lines {
		// Go through statements one at a time, seeing if we've hit
		// them or not.
		//
		// The prefix approach isn't perfect, as it doesn't
		// distinguish multiple statements per line. It'll have to
		// do for now. We could do fancy ANSI colouring later.
		lineno := lineno + i
		statementFound := false
		hit := false
		for j := 0; j < len(statements); j++ {
			start := file.Line(file.Pos(statements[j].Start))
			if start == lineno {
				statementFound = true
				if !hit && statements[j].Reached > 0 {
					hit = true
				}
				statements = statements[1:]
			} else {
				break
			}
		}
		hitmiss := hitPrefix
		if statementFound && !hit {
			hitmiss = missPrefix
		}
		if html {
			fmt.Fprint(w, "<SPAN class=\"")
			if statementFound && !hit {
				fmt.Fprint(w, htmlMissClass)
			} else {
				fmt.Fprint(w, htmlHitClass)
			}
			fmt.Fprintf(w, "\">%*d\t%s\n</SPAN>", linenoWidth, lineno, line)
		} else {
			fmt.Fprintf(w, "%*d %s\t%s\n", linenoWidth, lineno, hitmiss, line)
		}
	}
	fmt.Fprintln(w)
	if html {
		fmt.Fprintln(w, "</PRE>" + htmlFooter)
	}

	return nil
}
