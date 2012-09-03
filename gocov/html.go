// html.go
package main

import (
	"fmt"
	"io"
	"github.com/axw/gocov"
)

func htmlReport() (rc int) {
	html = true
	return reportCoverage()
}

//WIP Gocov Test Coverage Report
func printHeader(w io.Writer, title string) {
	if html {
		fmt.Fprintln(w, "<!DOCTYPE html>\n<HTML>\n<HEAD><meta http-equiv=\"Content-Type\" content=\"text/html; charset=utf-8\">")
		fmt.Fprintf(w, "<LINK HREF=\"gocov.css\" rel=\"stylesheet\"><TITLE> %s </TITLE></HEAD><BODY>", title)
	}
}

func printFooter(w io.Writer) {
	if html {
		fmt.Fprintln(w, "</BODY></HTML>")
	}
}

func printPackageHeader(w io.Writer, pkg *gocov.Package) {
	if html {
		fmt.Fprintf(w, "<H2>%s</H2>\n", pkg.Name)
		fmt.Fprintln(w, "<TABLE>")
	}
}

func printPackageFooter(w io.Writer, reached int, total int, percentage float64) {
	if html {
		fmt.Fprintf(w,"<TR><TD></TD><TD class=\"function\">Total coverage</TD><TD class=\"total\">%.2f%%</TD><TD class=\"total\">(%d/%d)</TD></TR>\n", percentage, reached, total)
		fmt.Fprintln(w, "</TABLE>\n")
	} else {
		fmt.Fprintf(w,"Total coverage: %.2f%% (%d/%d)\n", percentage, reached, total)
	}
	
}