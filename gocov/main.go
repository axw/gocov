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
	"encoding/json"
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
	fmt.Fprintf(os.Stderr, "Usage:\n\n\tgocov command [arguments]\n\n")
	fmt.Fprintf(os.Stderr, "The commands are:\n\n")
	fmt.Fprintf(os.Stderr, "\tannotate\n")
	fmt.Fprintf(os.Stderr, "\ttest\n")
	fmt.Fprintf(os.Stderr, "\treport\n")
	fmt.Fprintf(os.Stderr, "\n")
	flag.PrintDefaults()
	os.Exit(2)
}

var (
	testFlags       = flag.NewFlagSet("test", flag.ExitOnError)
	testExcludeFlag = testFlags.String(
		"exclude", "",
		"packages to exclude, separated by comma")
	verbose bool
)

func init() {
	testFlags.BoolVar(&verbose, "v", false, "verbose")
}

type instrumenter struct {
	gopath       string // temporary gopath
	excluded     []string
	instrumented map[string]*gocov.Package
}

func putenv(env []string, key, value string) []string {
	for i, s := range env {
		if strings.HasPrefix(s, key+"=") {
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
	// Certain, special packages should always be skipped.
	switch pkgpath {
	case "C":
		return nil
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "instrumenting package %q\n", pkgpath)
	}

	// Ignore explicitly excluded packages.
	if i := sort.SearchStrings(in.excluded, pkgpath); i < len(in.excluded) {
		if in.excluded[i] == pkgpath {
			return nil
		}
	}

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
		if _, done := in.instrumented[subpkgpath]; !done {
			err = in.instrumentPackage(subpkgpath)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func marshalJson(packages []*gocov.Package) ([]byte, error) {
	return json.Marshal(struct{ Packages []*gocov.Package }{packages})
}

func unmarshalJson(data []byte) (packages []*gocov.Package, err error) {
	result := &struct{ Packages []*gocov.Package }{}
	err = json.Unmarshal(data, result)
	if err == nil {
		packages = result.Packages
	}
	return
}

func instrumentAndTest() (rc int) {
	testFlags.Parse(os.Args[2:])
	packageName := "."
	if testFlags.NArg() > 0 {
		packageName = testFlags.Arg(0)
	}

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

	var excluded []string
	if len(*testExcludeFlag) > 0 {
		excluded = strings.Split(*testExcludeFlag, ",")
		sort.Strings(excluded)
	}

	in := &instrumenter{
		gopath:       tempDir,
		instrumented: make(map[string]*gocov.Package),
		excluded:     excluded}
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
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	err = cmd.Run()

	if err != nil {
		fmt.Fprintf(os.Stderr, "go test failed: %s\n", err)
		rc = 1
	}

	packages, err := gocov.ParseTrace(outfilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse gocov output: %s\n", err)
	} else {
		data, err := marshalJson(packages)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to format as JSON: %s\n", err)
		} else {
			fmt.Println(string(data))
		}
	}
	return
}

func main() {
	flag.Usage = usage
	flag.Parse()
	command := ""
	if flag.NArg() > 0 {
		command = flag.Arg(0)
		switch command {
		case "annotate":
			os.Exit(annotateSource())
		case "report":
			os.Exit(reportCoverage())
		case "test":
			os.Exit(instrumentAndTest())
		//case "run"
		default:
			fmt.Fprintf(os.Stderr, "Unknown command: %#q\n\n", command)
			usage()
		}
	} else {
		usage()
	}
}
