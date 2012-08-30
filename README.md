gocov
-----

Coverage testing tool for The Go Programming Language (work-in-progress)

Installation
============

```go get github.com/axw/gocov/gocov```

Usage
=====

There are currently three gocov commands: ```test```, ```report``` and ```annotate```.

*gocov test*

Running `gocov test <package>` will, for the specified package,
instrument all imported packages not part of the standard library,
and run "go test". Upon completion, coverage data will be emitted
in the form of a JSON document.

Packages will be recursively checked for imports, and those
packages will also be instrumented. If you wish to exclude a
package from instrumentation, you can specify an optional exclude
flag, e.g. `gocov test -exclude comma,separated,packages`.

*gocov report*

Running `gocov report <coverage.json>` will generate a textual
report from the coverage data output by `gocov test`. It is
assumed that the source code has not changed in between.

Output from ```gocov test``` is logged to stdout so users with 
POSIX compatible terminals can direct the output to ```gocov report``` 
to view a summary of the test coverage, for example: -

    gocov test mypackage | gocov report

*gocov annotate*

Running `gocov annotate <coverage.json> <package[.receiver].function>`
will generate a source listing of the specified function, annotating
it with coverage information, such as which lines have been missed.
