package main

import (
	"fmt"
	"io"
	"os"

	"github.com/fulcrus/hopclaw/internal/cli"
)

func printDeprecationNotice(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Note: 'openclaw' binary is deprecated. Use 'hopclaw' instead.")
}

func main() {
	printDeprecationNotice(os.Stderr)
	cli.Main()
}
