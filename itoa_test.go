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

package gocov

import (
	"fmt"
	"testing"
)

const maxint = int(^uint(0) >> 1)
const minint = -maxint - 1

func checkPanic(f func()) (panicked bool) {
	defer func() {
		if err := recover(); err != nil {
			panicked = true
		}
	}()
	f()
	return panicked
}

func TestItoa(t *testing.T) {
	var values = [...]int{
		0, 1, -1, 10, -10, 100, -100, maxint, minint + 1,
	}
	for _, v := range values {
		expected := fmt.Sprint(v)
		actual := itoa(v)
		if actual != expected {
			t.Errorf("expected %s, received %s", expected, actual)
		}
	}

	// minint will panic due to a known bug in itoa
	if !checkPanic(func() { itoa(minint) }) {
		t.Errorf("Expected itoa(%d) to panic", minint)
	}
}
