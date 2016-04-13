package gode

import (
	"fmt"
	"os"
)

func ExampleRunScript() {
	SetRootPath(os.TempDir())
	cmd, done := RunScript(`console.log("hello world!")`)
	output, err := cmd.CombinedOutput()
	if err != nil {
		panic(err)
	}
	fmt.Println(string(output))
	// Output:
	// hello world!
	done()
}
