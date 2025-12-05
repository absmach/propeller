package main

func predict(x0 int32, x1 int32) int32 {
    in := []float64{
        float64(x0) / 100.0,
        float64(x1) / 100.0,
    }

    y := score(in)

    return int32(y * 100.0)
}

func main() {}
