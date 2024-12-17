package main

import "time"

//export add
func add(x, y uint32) uint32 {
	time.Sleep(time.Minute)
	return x + y
}

// main is required for the `wasi` target, even if it isn't used.
// See https://wazero.io/languages/tinygo/#why-do-i-have-to-define-main
func main() {}
