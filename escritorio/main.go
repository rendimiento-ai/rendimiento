//go:build !windows

// rendimiento for Windows: the window, the icon next to the clock and the
// first-time assistant over rendimiento's WSL2 distribution. On any other
// system there is nothing for it to keep.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "rendimiento "+version+": this app is for Windows. On Linux and macOS, see https://rendimiento.ai")
	os.Exit(1)
}
