package convert

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExprName(t *testing.T) {
	source := `
package foo

func Function() {}
func (x Foo) Method() {}
func (x *Foo) PtrMethod() {}
func (x *Foo[T]) GenericMethod() {}
`

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "", source, 0)
	if err != nil {
		t.Fatal(err)
	}

	function0 := parsed.Decls[0].(*ast.FuncDecl)
	assert.Equal(t, "Function", functionName(function0))

	function1 := parsed.Decls[1].(*ast.FuncDecl)
	assert.Equal(t, "Foo.Method", functionName(function1))

	function2 := parsed.Decls[2].(*ast.FuncDecl)
	assert.Equal(t, "Foo.PtrMethod", functionName(function2))

	function3 := parsed.Decls[3].(*ast.FuncDecl)
	assert.Equal(t, "Foo[T].GenericMethod", functionName(function3))

}
