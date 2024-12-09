package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/absmach/propeller/proplet"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown on interrupt signal
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		cancel()
		fmt.Println("Shutting down gracefully...")
	}()

	// Load configuration
	config, err := proplet.LoadConfig("worker/config.json")
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize and run the Proplet service
	proplet, err := proplet.NewPropletService(ctx, config)
	if err != nil {
		fmt.Printf("Failed to initialize Proplet: %v\n", err)
		os.Exit(1)
	}

	if err := proplet.Run(ctx); err != nil {
		fmt.Printf("Error running Proplet: %v\n", err)
	}
}
