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
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

func capture(wd string, args []string) ([]byte, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = nil
	out := &bytes.Buffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = wd
	err := cmd.Run()

	out2 := &bytes.Buffer{}
	prefix := "warning: no packages being tested depend on "
	for _, line := range strings.SplitAfter(out.String(), "\n") {
		if !strings.HasPrefix(line, prefix) {
			out2.WriteString(line)
		}
	}
	return out2.Bytes(), err
}

func readDirNames(dirname string) []string {
	f, err := os.Open(dirname)
	if err != nil {
		return nil
	}
	names, err := f.Readdirnames(-1)
	_ = f.Close()
	return names
}

// relToGOPATH returns the path relative to $GOPATH/src.
func relToGOPATH(p string) (string, error) {
	for _, gopath := range filepath.SplitList(os.Getenv("GOPATH")) {
		if len(gopath) == 0 {
			continue
		}
		srcRoot := filepath.Join(gopath, "src")
		// TODO(maruel): Accept case-insensitivity on Windows/OSX, maybe call
		// filepath.EvalSymlinks().
		if !strings.HasPrefix(p, srcRoot) {
			continue
		}
		rel, err := filepath.Rel(srcRoot, p)
		if err != nil {
			return "", fmt.Errorf("failed to find relative path from %s to %s", srcRoot, p)
		}
		return rel, err
	}
	return "", fmt.Errorf("failed to find GOPATH relative directory for %s", p)
}

// goTestDirs returns the list of directories with '*_test.go' files.
func goTestDirs(root string) []string {
	dirsTestsFound := map[string]bool{}
	var recurse func(dir string)
	recurse = func(dir string) {
		for _, f := range readDirNames(dir) {
			if f[0] == '.' || f[0] == '_' {
				continue
			}
			p := filepath.Join(dir, f)
			stat, err := os.Stat(p)
			if err != nil {
				continue
			}
			if stat.IsDir() {
				recurse(p)
			} else {
				if strings.HasSuffix(p, "_test.go") {
					dirsTestsFound[dir] = true
				}
			}
		}
	}
	recurse(root)
	goTestDirs := make([]string, 0, len(dirsTestsFound))
	for d := range dirsTestsFound {
		goTestDirs = append(goTestDirs, d)
	}
	sort.Strings(goTestDirs)
	return goTestDirs
}

type result struct {
	out []byte
	err error
}

// First argument must be the relative package name.
func runTests(args []string) error {
	if len(args) != 0 && strings.HasSuffix(args[0], "...") {
		return runAllTests(args)
	}
	return runOneTest(args)
}

func runOneTest(args []string) error {
	coverprofile, err := ioutil.TempFile("", "gocov")
	if err != nil {
		return err
	}
	coverprofile.Close()
	defer os.Remove(coverprofile.Name())
	args = append([]string{
		"test", "-coverprofile", coverprofile.Name(),
	}, args...)
	cmd := exec.Command("go", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return convertProfiles(coverprofile.Name())
}

func runAllTests(args []string) (err error) {
	pkgRoot, _ := os.Getwd()
	pkg, err2 := relToGOPATH(pkgRoot)
	if err2 != nil {
		return err2
	}
	// TODO(maruel): This assumes this starts with "./". This is
	// incorrect,someone could request to run test in a separate package.
	requestedPath := filepath.Join(pkgRoot, args[0][:len(args[0])-3])
	testDirs := goTestDirs(requestedPath)
	if len(testDirs) == 0 {
		return nil
	}

	tmpDir, err2 := ioutil.TempDir("", "gocov")
	if err2 != nil {
		return err2
	}
	defer func() {
		err2 := os.RemoveAll(tmpDir)
		if err == nil {
			err = err2
		}
	}()

	// It passes a unique -coverprofile file name, so that all the files can
	// later be merged into a single file.
	var wg sync.WaitGroup
	results := make(chan *result, len(testDirs))
	for i, td := range testDirs {
		wg.Add(1)
		go func(index int, testDir string) {
			defer wg.Done()
			args := []string{
				"go", "test", "-covermode=count", "-coverpkg", pkg + "/...",
				"-coverprofile", filepath.Join(tmpDir, fmt.Sprintf("test%d.cov", index)),
			}
			out, err := capture(testDir, args)
			results <- &result{out, err}
		}(i, td)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	for result := range results {
		os.Stderr.Write(result.out)
		if err == nil && result.err != nil {
			err = result.err
		}
	}

	// Merge the profiles. Sums all the counts.
	// Format is "file.go:XX.YY,ZZ.II J K"
	// J is number of statements, K is count.
	files, err2 := filepath.Glob(filepath.Join(tmpDir, "test*.cov"))
	if err2 != nil {
		return err2
	}
	return convertProfiles(files...)
}
