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
const instrumentedPathPrefix = gocovPackagePath + "/instrumented"

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
	testExcludeGorootFlag = testFlags.Bool(
		"exclude-goroot", true,
		"exclude packages in GOROOT from instrumentation")
	testWorkFlag = testFlags.Bool(
		"work", false,
		"print the name of the temporary work directory "+
			"and do not delete it when exiting")
	verbose bool
)

func init() {
	testFlags.BoolVar(&verbose, "v", false, "verbose")
}

func errorf(f string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, f, args...)
}

func verbosef(f string, args ...interface{}) {
	if verbose {
		fmt.Fprintf(os.Stderr, f, args...)
	}
}

type instrumenter struct {
	gopath       string // temporary gopath
	excluded     []string
	instrumented map[string]*gocov.Package
	processed    map[string]struct{}
	workingdir   string // path of package currently being processed
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

func (in *instrumenter) parsePackage(path string, fset *token.FileSet) (*build.Package, *ast.Package, error) {
	p, err := build.Import(path, in.workingdir, 0)
	if err != nil {
		return nil, nil, err
	}
	goFiles := append(p.GoFiles[:], p.CgoFiles...)
	sort.Strings(goFiles)
	filter := func(f os.FileInfo) bool {
		name := f.Name()
		i := sort.SearchStrings(goFiles, name)
		return i < len(goFiles) && goFiles[i] == name
	}
	mode := parser.DeclarationErrors | parser.ParseComments
	pkgs, err := parser.ParseDir(fset, p.Dir, filter, mode)
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
		if _, err = os.Stat(target); err == nil {
			return nil
		}

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

// FIXME(axw)
//
// As we change the name of the package to avoid being obscured by GOROOT, we
// can't instrument packages that have Go functions implemented in C, such as
// those below.
//
// It may be preferable to have a faked GOROOT, but we'll need to come up with
// a way to avoid the import loop introduced by importing gocov into the
// instrumented pakcage.
//
func instrumentable(path string) bool {
	switch path {
	case "C":
		fallthrough
	case "reflect":
		fallthrough
	case "runtime":
		fallthrough
	case "os":
		fallthrough
	case "sync":
		fallthrough
	case "syscall":
		fallthrough
	case "time":
		fallthrough
	case "testing":
		fallthrough
	case "unsafe":
		return false
	}
	return true
}

// abspkgpath converts a possibly local import path to an absolute package path.
func (in *instrumenter) abspkgpath(pkgpath string) (error, string) {
	if pkgpath == "C" || pkgpath == "unsafe" {
		return nil, pkgpath
	}
	p, err := build.Import(pkgpath, in.workingdir, build.FindOnly)
	if err != nil {
		return err, ""
	}
	return nil, p.ImportPath
}

func (in *instrumenter) instrumentPackage(pkgpath string, testPackage bool) error {
	if _, already := in.processed[pkgpath]; already {
		return nil
	}
	defer func() {
		if _, instrumented := in.instrumented[pkgpath]; !instrumented {
			in.processed[pkgpath] = struct{}{}
		}
	}()

	// Certain packages should always be skipped.
	if !instrumentable(pkgpath) {
		verbosef("skipping uninstrumentable package %q\n", pkgpath)
		return nil
	}

	// Ignore explicitly excluded packages.
	if i := sort.SearchStrings(in.excluded, pkgpath); i < len(in.excluded) {
		if in.excluded[i] == pkgpath {
			verbosef("skipping excluded package %q\n", pkgpath)
			return nil
		}
	}

	fset := token.NewFileSet()
	buildpkg, pkg, err := in.parsePackage(pkgpath, fset)
	if err != nil {
		return err
	}
	if buildpkg.Goroot && *testExcludeGorootFlag {
		verbosef("skipping GOROOT package %q\n", pkgpath)
		return nil
	}

	in.instrumented[pkgpath] = nil // created in first instrumented file
	verbosef("instrumenting package %q\n", pkgpath)

	if testPackage && len(buildpkg.TestGoFiles)+len(buildpkg.XTestGoFiles) == 0 {
		return fmt.Errorf("no test files")
	}

	// Set a "working directory", for resolving relative imports.
	defer func(oldworkingdir string) {
		in.workingdir = oldworkingdir
	}(in.workingdir)
	in.workingdir = buildpkg.Dir

	imports := buildpkg.Imports[:]
	if testPackage {
		imports = append(imports, buildpkg.TestImports...)
		imports = append(imports, buildpkg.XTestImports...)
	}
	for _, subpkgpath := range imports {
		err, subpkgpath = in.abspkgpath(subpkgpath)
		if err != nil {
			return err
		}
		if _, done := in.instrumented[subpkgpath]; !done {
			err = in.instrumentPackage(subpkgpath, false)
			if err != nil {
				return err
			}
		}
	}

	// Fix imports in test files, but don't instrument them.
	rewriteFiles := make(map[string]*ast.File)
	if testPackage {
		testGoFiles := buildpkg.TestGoFiles[:]
		testGoFiles = append(testGoFiles, buildpkg.XTestGoFiles...)
		for _, filename := range testGoFiles {
			path := filepath.Join(buildpkg.Dir, filename)
			mode := parser.DeclarationErrors | parser.ParseComments
			file, err := parser.ParseFile(fset, path, nil, mode)
			if err != nil {
				return err
			}
			in.redirectImports(file)
			rewriteFiles[filename] = file
		}
	}

	// Clone the directory structure, symlinking files (if possible),
	// otherwise copying the files. Instrumented files will replace
	// the symlinks with new files.
	cloneDir := filepath.Join(in.gopath, "src", instrumentedPathPrefix, pkgpath)
	err = symlinkHierarchy(buildpkg.Dir, cloneDir)
	if err != nil {
		return err
	}
	// pkg == nil if there are only test files.
	if pkg != nil {
		for filename, f := range pkg.Files {
			err := in.instrumentFile(f, fset, pkgpath)
			if err != nil {
				return err
			}
			rewriteFiles[filename] = f
		}
	}
	for filename, f := range rewriteFiles {
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
		errorf("failed to create temporary GOPATH: %s\n", err)
		return 1
	}
	if *testWorkFlag {
		fmt.Fprintf(os.Stderr, "WORK=%s\n", tempDir)
	} else {
		defer func() {
			err := os.RemoveAll(tempDir)
			if err != nil {
				fmt.Fprintf(os.Stderr,
					"warning: failed to delete temporary GOPATH (%s)\n", tempDir)
			}
		}()
	}

	err = os.Mkdir(filepath.Join(tempDir, "src"), 0700)
	if err != nil {
		errorf("failed to create temporary src directory: %s\n", err)
		return 1
	}

	// Copy gocov into the temporary GOPATH, since otherwise it'll
	// be eclipsed by the instrumented packages root.
	if p, err := build.Import(gocovPackagePath, "", build.FindOnly); err == nil {
		err = symlinkHierarchy(p.Dir, filepath.Join(tempDir, "src", gocovPackagePath))
		if err != nil {
			errorf("failed to symlink gocov: %s\n", err)
			return 1
		}
	} else {
		errorf("failed to locate gocov: %s\n", err)
		return 1
	}

	var excluded []string
	if len(*testExcludeFlag) > 0 {
		excluded = strings.Split(*testExcludeFlag, ",")
		sort.Strings(excluded)
	}

	cwd, err := os.Getwd()
	if err != nil {
		errorf("failed to determine current working directory: %s\n", err)
	}

	in := &instrumenter{
		gopath:       tempDir,
		instrumented: make(map[string]*gocov.Package),
		excluded:     excluded,
		processed:    make(map[string]struct{}),
		workingdir:   cwd}
	err, packageName = in.abspkgpath(packageName)
	if err != nil {
		errorf("failed to resolve package path(%s): %s\n", err)
		return 1
	}
	err = in.instrumentPackage(packageName, true)
	if err != nil {
		errorf("failed to instrument package(%s): %s\n", packageName, err)
		return 1
	}

	ninstrumented := 0
	for _, pkg := range in.instrumented {
		if pkg != nil {
			ninstrumented++
		}
	}
	if ninstrumented == 0 {
		errorf("error: no packages were instrumented\n")
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
	args := []string{"test"}
	if verbose {
		args = append(args, "-v")
	}
	args = append(args, instrumentedPathPrefix+"/"+packageName)
	cmd := exec.Command("go", args...)
	cmd.Env = env
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	err = cmd.Run()

	if err != nil {
		errorf("go test failed: %s\n", err)
		rc = 1
	}

	packages, err := gocov.ParseTrace(outfilePath)
	if err != nil {
		errorf("failed to parse gocov output: %s\n", err)
		rc = 1
	} else {
		data, err := marshalJson(packages)
		if err != nil {
			errorf("failed to format as JSON: %s\n", err)
			rc = 1
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
