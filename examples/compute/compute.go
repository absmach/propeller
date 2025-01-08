package main

import "math"

//export compute
func compute(n uint32) uint32 {
	var result uint32

	for i := range n {
		for j := range n {
			for k := range n {
				for l := range n {
					for m := range n {
						// Do some meaningless but CPU-intensive math
						result += uint32(math.Pow(float64(i*j*k*l*m), 2)) % 10
					}
				}
			}
		}
	}

	return result
}

// main is required for the `wasi` target, even if it isn't used.
// See https://wazero.io/languages/tinygo/#why-do-i-have-to-define-main
func main() {}
