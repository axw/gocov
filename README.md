# gocov

Coverage testing tool for The Go Programming Language

[![Build Status][1]][2]

[1]: https://secure.travis-ci.org/axw/gocov.png
[2]: http://www.travis-ci.org/axw/gocov

## Installation

```go get github.com/axw/gocov/gocov```

## Usage

There are currently three gocov commands: ```test```, ```report``` and ```annotate```.

#### gocov test

Running `gocov test <package>` will, for the specified package,
instrument all imported packages not part of the standard library,
and run "go test". Upon completion, coverage data will be emitted
in the form of a JSON document.

###### Controlling instrumentation

By default only the specified package will be instrumented and
consequently have coverage information provided for. There are
several flags that can change this behaviour.

By running `gocov test -deps <package>` you direct gocov to recursively
instrument package dependencies, in which case coverage information
will be provided for all dependencies as well. The coverage information
provided is relative only to the tests run in the originally specified
package.

e.g. If you run `gocov test -deps net/http`, then you will see
coverage information not just for `net/http`, but also its dependencies;
the output tells you what code in the dependencies is exercised when the
`net/http` tests are run.

If you specify `-deps` but wish to exclude a specific package from
instrumentation, you can pass an additional `-exclude` flag, e.g.
`gocov test -deps -exclude comma,separated,packages`. If you wish to
exclude all packages in GOROOT, then you can use the shortcut
`-exclude-goroot` flag instead.

#### gocov report

Running `gocov report <coverage.json>` will generate a textual
report from the coverage data output by `gocov test`. It is
assumed that the source code has not changed in between.

Output from ```gocov test``` is logged to stdout so users with 
POSIX compatible terminals can direct the output to ```gocov report``` 
to view a summary of the test coverage, for example: -

    gocov test mypackage | gocov report

#### gocov annotate

Running `gocov annotate <coverage.json> <package[.receiver].function>`
will generate a source listing of the specified function, annotating
it with coverage information, such as which lines have been missed.

## Related tools

[GoCovGUI](http://github.com/nsf/gocovgui/):
A simple GUI wrapper for the gocov coverage analysis tool.

[gocov-html](https://github.com/matm/gocov-html):
A simple helper tool for generating HTML output from gocov.
