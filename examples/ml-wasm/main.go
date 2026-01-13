package main

// This example demonstrates a simple ML inference workload that can be compiled to WASM.
// The model is a linear regression trained to approximate: y = 2*x0 + 3*x1
//
// How to build and run:
//
// 1. Generate the model:
//    cd examples/ml-wasm
//    python3 train_model.py
//    # This generates mymodel.pkl (pickled sklearn model)
//
// 2. Generate Go code from the model:
//    go run model_gen.go > model_gen_output.go
//    # Or manually extract coefficients from mymodel.pkl and update model_gen.go
//
// 3. Build as WASM:
//    GOOS=wasip1 GOARCH=wasm go build -o mymodel.wasm main.go model_gen.go
//
// 4. Use with Propeller:
//    # The WASM module can be deployed via Propeller manager
//    # It exports a predict() function that takes two int32 inputs and returns int32
//
// Example usage in Propeller:
//   - Deploy mymodel.wasm via manager
//   - Call predict(x0, x1) where x0 and x1 are scaled by 100 (e.g., 200 = 2.0)
//   - Returns prediction scaled by 100 (e.g., 700 = 7.0)

func predict(x0 int32, x1 int32) int32 {
	// Scale inputs from int32 (scaled by 100) to float64
	in := []float64{
		float64(x0) / 100.0,
		float64(x1) / 100.0,
	}

	// Get model prediction
	y := score(in)

	// Scale output back to int32 (multiply by 100)
	return int32(y * 100.0)
}

func main() {
	// Empty main - this module is designed to be imported/called by Propeller runtime
	// The predict() function is the entry point for inference
}
