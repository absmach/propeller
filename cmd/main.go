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

	// Initialize and run the Proplet service
	service, err := initializeProplet(ctx, "proplet/config.json")
	if err != nil {
		fmt.Printf("Error initializing Proplet: %v\n", err)
		os.Exit(1)
	}

	if err := service.Run(ctx); err != nil {
		fmt.Printf("Error running Proplet: %v\n", err)
	}
}

func initializeProplet(ctx context.Context, configPath string) (*proplet.PropletService, error) {
	config, err := proplet.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	propletService, err := proplet.NewPropletService(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Proplet: %w", err)
	}

	return propletService, nil
}
