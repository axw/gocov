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
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const gocovPackagePath = "github.com/axw/gocov"

func usage() {
	fmt.Fprintf(os.Stderr, "Usage:\n\n\tgocov command [package]\n\n")
	fmt.Fprintf(os.Stderr, "The commands are:\n\n")
	fmt.Fprintf(os.Stderr, "\ttest\n")
	fmt.Fprintf(os.Stderr, "\n")
	flag.PrintDefaults()
	os.Exit(2)
}

type instrumenter struct {
	gopath       string // temporary gopath
	instrumented map[string]*gocov.Package
}

func putenv(env []string, key, value string) []string {
	for i, s := range env {
		if strings.HasPrefix(s, "GOPATH=") {
			env[i] = key + "=" + value
			return env
		}
	}
	return append(env, key+"="+value)
}

func parsePackage(path string, fset *token.FileSet) (*build.Package, *ast.Package, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	p, err := build.Import(path, cwd, 0)
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(p.GoFiles)
	filter := func(f os.FileInfo) bool {
		name := f.Name()
		i := sort.SearchStrings(p.GoFiles, name)
		return i < len(p.GoFiles) && p.GoFiles[i] == name
	}
	pkgs, err := parser.ParseDir(fset, p.Dir, filter, parser.DeclarationErrors)
	if err != nil {
		return nil, nil, err
	}
	return p, pkgs[p.Name], err
}

func symlinkHierarchy(src, dst string) error {
	fn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0700)
		} else {
			err = os.Symlink(path, target)
			if err != nil {
				srcfile, err := os.Open(path)
				if err != nil {
					return err
				}
				defer srcfile.Close()
				dstfile, err := os.OpenFile(
					target, os.O_RDWR|os.O_CREATE, 0600)
				if err != nil {
					return err
				}
				defer dstfile.Close()
				_, err = io.Copy(dstfile, srcfile)
				return err
			}
		}
		return nil
	}
	return filepath.Walk(src, fn)
}

func (in *instrumenter) instrumentPackage(pkgpath string) error {
	fset := token.NewFileSet()
	buildpkg, pkg, err := parsePackage(pkgpath, fset)
	if err != nil {
		return err
	}
	in.instrumented[pkgpath] = nil // created in first instrumented file
	if buildpkg.Goroot {
		// ignore packages in GOROOT
		return nil
	}

	// Clone the directory structure, symlinking files (if possible),
	// otherwise copying the files. Instrumented files will replace
	// the symlinks with new files.
	cloneDir := filepath.Join(in.gopath, "src", pkgpath)
	err = symlinkHierarchy(buildpkg.Dir, cloneDir)

	for filename, f := range pkg.Files {
		err := in.instrumentFile(f, fset)
		if err != nil {
			return err
		}

		if err == nil {
			filepath := filepath.Join(cloneDir, filepath.Base(filename))
			err = os.Remove(filepath)
			if err != nil {
				return err
			}
			file, err := os.OpenFile(filepath, os.O_RDWR|os.O_CREATE, 0600)
			if err != nil {
				return err
			}
			//printer.Fprint(os.Stdout, fset, f) // TODO check err?
			printer.Fprint(file, fset, f) // TODO check err?
			err = file.Close()
			if err != nil {
				return err
			}
		}
		if err != nil {
			return err
		}
	}

	// TODO include/exclude package names with a pattern.
	for _, subpkgpath := range buildpkg.Imports {
		// Make sure the package hasn't been instrumented yet (may be
		// a transitive dependency).
		if _, done := in.instrumented[subpkgpath]; !done {
			err = in.instrumentPackage(subpkgpath)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func instrumentAndTest(packageName string) (rc int) {
	tempDir, err := ioutil.TempDir("", "gocov")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temporary GOPATH: %s", err)
		return 1
	}
	defer func() {
		err := os.RemoveAll(tempDir)
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"warning: failed to delete temporary GOPATH (%s)", tempDir)
		}
	}()

	err = os.Mkdir(filepath.Join(tempDir, "src"), 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"failed to create temporary src directory: %s", err)
		return 1
	}

	in := &instrumenter{
		gopath:       tempDir,
		instrumented: make(map[string]*gocov.Package)}
	err = in.instrumentPackage(packageName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to instrument package(%s): %s\n",
			packageName, err)
		return 1
	}

	// Run "go test".
	// TODO pass through test flags.
	outfilePath := filepath.Join(tempDir, "gocov.out")
	env := os.Environ()
	env = putenv(env, "GOCOVOUT", outfilePath)
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		gopath = fmt.Sprintf("%s%c%s", tempDir, os.PathListSeparator, gopath)
		env = putenv(env, "GOPATH", gopath)
	} else {
		env = putenv(env, "GOPATH", tempDir)
	}
	cmd := exec.Command("go", "test", "-v", packageName)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()

	if err != nil {
		fmt.Fprintf(os.Stderr, "go test failed: %s\n", err)
		rc = 1
	}

	packages, err := gocov.ParseFile(outfilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse gocov output: %s\n", err)
	} else {
		report := gocov.NewReport()
		for _, pkg := range packages {
			report.AddPackage(pkg)
		}
		fmt.Println()
		gocov.PrintReport(os.Stdout, report)
	}
	return
}

func main() {
	flag.Usage = usage
	flag.Parse()
	command := ""
	packageName := "."
	if flag.NArg() > 0 {
		command = flag.Arg(0)
		if command != "test" { //&& command != "run" {
			fmt.Fprintf(os.Stderr, "Unknown command: %#q\n\n", command)
			usage()
		}
		if flag.NArg() > 1 {
			packageName = flag.Arg(1)
		}
	} else {
		usage()
	}
	// TODO create and pass a "Command" interface.
	os.Exit(instrumentAndTest(packageName))
}
