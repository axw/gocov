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
	"errors"
	"flag"
	"fmt"
	"github.com/axw/gocov"
	"github.com/axw/gocov/gocovutil"
	"github.com/axw/gocov/parser"
	"go/ast"
	"go/build"
	goparser "go/parser"
	"go/printer"
	"go/token"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const gocovPackagePath = "github.com/axw/gocov"
const instrumentedGocovPackagePath = "github.com/axw/gocov/instrumented"
const unmanagedPackagePathRoot = "github.com/axw/gocov/unmanaged"

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
	testFlags    = flag.NewFlagSet("test", flag.ExitOnError)
	testDepsFlag = testFlags.Bool(
		"deps", false,
		"Instrument all package dependencies, including transitive")
	testExcludeFlag = testFlags.String(
		"exclude", "",
		"packages to exclude, separated by comma")
	testExcludeGorootFlag = testFlags.Bool(
		"exclude-goroot", false,
		"exclude packages in GOROOT from instrumentation")
	testWorkFlag = testFlags.Bool(
		"work", false,
		"print the name of the temporary work directory "+
			"and do not delete it when exiting")
	testTagsFlag = testFlags.String(
		"tags", "",
		"a list of build tags to consider satisfied during the build")
	testRunFlag = testFlags.String(
		"run", "",
		"Run only those tests and examples matching the regular "+
			"expression.")
	testTimeoutFlag = testFlags.String(
		"timeout", "", "If a test runs longer than t, panic.")
	testParallelFlag = testFlags.Int(
		"parallel", runtime.GOMAXPROCS(-1), "Run test in parallel (see: go help testflag)")
	testPackageParallelFlag = testFlags.Int(

		// See: golang.org/src/cmd/go/testflag.go the '-p' flag seems to be "undocumented", it is in a usage message,
		// but I haven't worked out what command to run to display that message
		// The default is 2 since that seems to be the default for 'go test' when GOMAXPROCS is 1
		"p", runtime.GOMAXPROCS(-1)+1, "Run test packages in parallel (see: golang.org/src/cmd/go/testflag.go)")
	verbose  bool
	verboseX bool
)

func init() {
	testFlags.BoolVar(&verbose, "v", false, "be verbose")
	testFlags.BoolVar(&verboseX, "x", false, "be verbose and print the commands")
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
	goroot       string // temporary GOROOT
	context      build.Context
	excluded     []string
	instrumented map[string]*gocov.Package
	processed    map[string]bool
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
	p, err := in.context.Import(path, in.workingdir, 0)
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
	mode := goparser.DeclarationErrors | goparser.ParseComments
	pkgs, err := goparser.ParseDir(fset, p.Dir, filter, mode)
	if err != nil {
		return nil, nil, err
	}
	return p, pkgs[p.Name], err
}

func symlinkHierarchy(src, dst string) error {
	// First check if the destination exists; if so, bail out
	// before doing a potentially expensive walk.
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
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

		// Walk directory symlinks. Check for target
		// existence above and os.MkdirAll below guards
		// against infinite recursion.
		mode := info.Mode()
		if mode&os.ModeSymlink == os.ModeSymlink {
			realpath, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if !filepath.IsAbs(realpath) {
				dir := filepath.Dir(path)
				realpath = filepath.Join(dir, realpath)
			}
			info, err := os.Stat(realpath)
			if err != nil {
				return err
			}
			if info.IsDir() {
				err = os.MkdirAll(target, 0700)
				if err != nil {
					return err
				}
				// Symlink contents, as the MkdirAll above
				// and the initial existence check will work
				// against each other.
				f, err := os.Open(realpath)
				if err != nil {
					return err
				}
				names, err := f.Readdirnames(-1)
				f.Close()
				if err != nil {
					return err
				}
				for _, name := range names {
					realpath := filepath.Join(realpath, name)
					target := filepath.Join(target, name)
					err = symlinkHierarchy(realpath, target)
					if err != nil {
						return err
					}
				}
				return nil
			}
		}

		if mode.IsDir() {
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

// The only packages that can't be instrumented are those that the core gocov
// package depends upon (and fake packages like C, unsafe).
func instrumentable(path string) bool {
	switch path {
	case "C", "runtime", "sync", "sync/atomic", "syscall", "unsafe":
		// Can't instrument the packages that gocov depends on.
		return false
	}
	return true
}

// abspkgpath converts a possibly local import path to an absolute package path.
func (in *instrumenter) abspkgpath(pkgpath string) (string, error) {
	if pkgpath == "C" || pkgpath == "unsafe" {
		return pkgpath, nil
	}
	p, err := in.context.Import(pkgpath, in.workingdir, build.FindOnly)
	if err != nil {
		return "", err
	}
	if build.IsLocalImport(p.ImportPath) {
		// If a local import was provided to go/build, but
		// it exists inside a GOPATH, go/build will fill in
		// the GOPATH import path. Otherwise it will remain
		// a local import path.
		err = errors.New(`
Coverage testing of packages outside of GOPATH is not currently supported.
See: https://github.com/axw/gocov/issues/30
`)
		return "", err
	}
	return p.ImportPath, nil
}

func instrumentedPackagePath(pkgpath string) string {
	// All instrumented packages import gocov. If we want to
	// instrumented gocov itself, we must change its name so
	// it can import (the uninstrumented version of) itself.
	if pkgpath == gocovPackagePath {
		return instrumentedGocovPackagePath
	}
	return pkgpath
}

func (in *instrumenter) instrumentPackage(pkgpath string, testPackage bool) error {
	if already := in.processed[pkgpath]; already {
		return nil
	}
	defer func() {
		if _, instrumented := in.instrumented[pkgpath]; !instrumented {
			in.processed[pkgpath] = true
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
	if !testPackage && (buildpkg.Goroot && *testExcludeGorootFlag) {
		verbosef("skipping GOROOT package %q\n", pkgpath)
		return nil
	}

	in.instrumented[pkgpath] = nil // created in first instrumented file
	verbosef("instrumenting package %q\n", pkgpath)

	if testPackage && len(buildpkg.TestGoFiles)+len(buildpkg.XTestGoFiles) == 0 {
		verbosef("skipping %q because no test files\n", buildpkg.Name)
		return nil
	}

	// Set a "working directory", for resolving relative imports.
	defer func(oldworkingdir string) {
		in.workingdir = oldworkingdir
	}(in.workingdir)
	in.workingdir = buildpkg.Dir

	if *testDepsFlag {
		imports := buildpkg.Imports[:]
		if testPackage {
			imports = append(imports, buildpkg.TestImports...)
			imports = append(imports, buildpkg.XTestImports...)
		}
		for _, subpkgpath := range imports {
			subpkgpath, err = in.abspkgpath(subpkgpath)
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
	}

	// Fix imports in test files, but don't instrument them.
	rewriteFiles := make(map[string]*ast.File)
	if testPackage {
		testGoFiles := buildpkg.TestGoFiles[:]
		testGoFiles = append(testGoFiles, buildpkg.XTestGoFiles...)
		for _, filename := range testGoFiles {
			path := filepath.Join(buildpkg.Dir, filename)
			mode := goparser.DeclarationErrors | goparser.ParseComments
			file, err := goparser.ParseFile(fset, path, nil, mode)
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
	//
	// If we instrument package x/y, and x/y uses package x, then
	// we must be sure to also symlink the files in package x,
	// as it'll be picked up in the instrumented GOROOT.
	parts := strings.Split(pkgpath, "/")
	var partpath string
	for i, part := range parts {
		if i == 0 {
			partpath = part
		} else {
			partpath += "/" + part
		}
		p, err := in.context.Import(partpath, in.workingdir, 0)
		if err != nil && i+1 == len(parts) {
			return err
		}
		if err == nil && p.SrcRoot == buildpkg.SrcRoot {
			ipkgpath := instrumentedPackagePath(partpath)
			clonedir := filepath.Join(in.goroot, "src", "pkg", ipkgpath)
			err = symlinkHierarchy(p.Dir, clonedir)
			if err != nil {
				return err
			}
			// You might think we can break here, but we can't;
			// since "instrumentedPackagePath" may change the
			// package path between source and destination, we
			// must check each level in the hierarchy individually.
		}
	}

	// pkg == nil if there are only test files.
	ipkgpath := instrumentedPackagePath(pkgpath)
	clonedir := filepath.Join(in.goroot, "src", "pkg", ipkgpath)
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
		filepath := filepath.Join(clonedir, filepath.Base(filename))
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

// packagesAndTestargs returns a list of package paths
// and a list of arguments to pass on to "go test".
func packagesAndTestargs() ([]string, []string, error) {
	// Everything before the first arg starting with "-"
	// is considered a package, and everything after/including
	// is an argument to pass on to "go test".
	var packagepaths, gotestargs []string
	if testFlags.NArg() > 0 {
		split := -1
		args := testFlags.Args()
		for i, arg := range args {
			if strings.HasPrefix(arg, "-") {
				split = i
				break
			}
		}
		if split >= 0 {
			packagepaths = args[:split]
			gotestargs = args[split:]
		} else {
			packagepaths = args
		}
	}
	if len(packagepaths) == 0 {
		packagepaths = []string{"."}
	}

	// Run "go list <packagepaths>" to expand "...", evaluate
	// "std", "all", etc. Also, "go list" collapses duplicates.
	args := []string{"list"}
	if *testTagsFlag != "" {
		tags := strings.Fields(*testTagsFlag)
		args = append(args, "-tags")
		args = append(args, tags...)
	}
	args = append(args, packagepaths...)
	output, err := exec.Command("go", args...).CombinedOutput()
	if err != nil {
		fmt.Fprintln(os.Stderr, string(output))
		return nil, nil, err
	} else {
		packagepaths = strings.Fields(string(output))
	}

	// FIXME we don't currently handle testing cmd/*, as they
	// are not regular packages. This means "gocov test std"
	// can't work unless we ignore cmd/* or until we implement
	// support.
	var prunedpaths []string
	for _, p := range packagepaths {
		if !strings.HasPrefix(p, "cmd/") {
			prunedpaths = append(prunedpaths, p)
		} else {
			fmt.Fprintf(os.Stderr, "warning: support for cmd/* not supported, ignoring %s\n", p)
		}
	}
	packagepaths = prunedpaths

	return packagepaths, gotestargs, nil
}

func instrumentAndTest() (rc int) {
	testFlags.Parse(os.Args[2:])
	packagePaths, gotestArgs, err := packagesAndTestargs()
	if err != nil {
		errorf("failed to process package list: %s\n", err)
		return 1
	}

	tempDir, err := ioutil.TempDir("", "gocov")
	if err != nil {
		errorf("failed to create temporary GOROOT: %s\n", err)
		return 1
	}
	if *testWorkFlag {
		fmt.Fprintf(os.Stderr, "WORK=%s\n", tempDir)
	} else {
		defer func() {
			err := os.RemoveAll(tempDir)
			if err != nil {
				fmt.Fprintf(os.Stderr,
					"warning: failed to delete temporary GOROOT (%s)\n", tempDir)
			}
		}()
	}

	goroot := runtime.GOROOT()
	for _, name := range [...]string{"src", "pkg"} {
		dir := filepath.Join(goroot, name)
		err = symlinkHierarchy(dir, filepath.Join(tempDir, name))
		if err != nil {
			errorf("failed to create $GOROOT/%s: %s\n", name, err)
			return 1
		}
	}

	// Copy gocov into the temporary GOROOT, since otherwise it'll
	// be eclipsed by the instrumented packages root. Use the default
	// build context here since gocov doesn't use custom build tags.
	if p, err := build.Import(gocovPackagePath, "", build.FindOnly); err == nil {
		err = symlinkHierarchy(p.Dir, filepath.Join(tempDir, "src", "pkg", gocovPackagePath))
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

	context := build.Default
	if *testTagsFlag != "" {
		context.BuildTags = strings.Fields(*testTagsFlag)
	}

	in := &instrumenter{
		goroot:       tempDir,
		context:      context,
		instrumented: make(map[string]*gocov.Package),
		excluded:     excluded,
		processed:    make(map[string]bool),
		workingdir:   cwd,
	}

	instrumentedPackagePaths := make([]string, len(packagePaths))
	for i, packagePath := range packagePaths {
		var absPackagePath string
		absPackagePath, err = in.abspkgpath(packagePath)
		if err != nil {
			errorf("failed to resolve package path(%s): %s\n", packagePath, err)
			return 1
		}
		packagePath = absPackagePath
		err = in.instrumentPackage(packagePath, true)
		if err != nil {
			errorf("failed to instrument package(%s): %s\n", packagePath, err)
			return 1
		}
		instrumentedPackagePaths[i] = instrumentedPackagePath(packagePath)
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

	// Temporarily rename package archives in
	// $GOPATH, for instrumented packages.
	for _, packagePath := range packagePaths {
		p, err := in.context.Import(packagePath, in.workingdir, build.FindOnly)
		if err == nil && !p.Goroot && p.PkgObj != "" {
			verbosef("temporarily renaming package object %s\n", p.PkgObj)
			err = os.Rename(p.PkgObj, p.PkgObj+".gocov")
			if err != nil {
				verbosef(" - failed to rename: %v\n", err)
			} else {
				defer func(pkgobj string) {
					verbosef("restoring package object %s\n", pkgobj)
					err = os.Rename(pkgobj+".gocov", pkgobj)
					if err != nil {
						verbosef(" - failed to restore package object: %v\n", err)
					}
				}(p.PkgObj)
			}
		}
	}

	// Run "go test".
	const gocovOutPrefix = "gocov.out"
	env := os.Environ()
	env = putenv(env, "GOCOVOUT", filepath.Join(tempDir, gocovOutPrefix))
	env = putenv(env, "GOROOT", tempDir)

	args := []string{"test"}
	if verbose {
		args = append(args, "-v")
	}
	if verboseX {
		args = append(args, "-x")
	}
	if *testTagsFlag != "" {
		args = append(args, "-tags", *testTagsFlag)
	}
	if *testRunFlag != "" {
		args = append(args, "-run", *testRunFlag)
	}
	if *testTimeoutFlag != "" {
		args = append(args, "-timeout", *testTimeoutFlag)
	}
	args = append(args, "-parallel", fmt.Sprint(*testParallelFlag))
	args = append(args, "-p", fmt.Sprint(*testPackageParallelFlag))
	args = append(args, instrumentedPackagePaths...)
	args = append(args, gotestArgs...)

	// First run with "-i" to avoid the warning
	// about out-of-date packages.
	testiargs := append([]string{args[0], "-i"}, args[1:]...)
	cmd := exec.Command("go", testiargs...)
	cmd.Env = env
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		errorf("go test -i failed: %s\n", err)
		return 1
	} else {
		// Now run "go test" normally.
		cmd = exec.Command("go", args...)
		cmd.Env = env
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			errorf("go test failed: %s\n", err)
			return 1
		}
	}

	tempDirFile, err := os.Open(tempDir)
	if err != nil {
		errorf("failed to open output directory: %s\n", err)
		return 1
	}
	defer tempDirFile.Close()

	names, err := tempDirFile.Readdirnames(-1)
	if err != nil {
		errorf("failed to list output directory: %s\n", err)
		return 1
	}

	var allpackages gocovutil.Packages
	for _, name := range names {
		if !strings.HasPrefix(name, gocovOutPrefix) {
			continue
		}
		outfilePath := filepath.Join(tempDir, name)
		packages, err := parser.ParseTrace(outfilePath)
		if err != nil {
			errorf("failed to parse gocov output: %s\n", err)
			return 1
		}
		for _, p := range packages {
			allpackages.AddPackage(p)
		}
	}

	data, err := marshalJson(allpackages)
	if err != nil {
		errorf("failed to format as JSON: %s\n", err)
		return 1
	} else {
		fmt.Println(string(data))
	}
	return
}

func main() {
	flag.Usage = usage
	flag.Parse()
	if verboseX {
		verbose = true
	}

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
