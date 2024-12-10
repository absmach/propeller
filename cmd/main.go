package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	// Check connectivity to App Registry
	if err := checkRegistryConnectivity(config.RegistryURL); err != nil {
		return nil, fmt.Errorf("failed connectivity check for App Registry: %w", err)
	}

	propletService, err := proplet.NewPropletService(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Proplet: %w", err)
	}

	fmt.Printf("App Registry configured: %s\n", config.RegistryURL)
	return propletService, nil
}

// checkRegistryConnectivity tests if the Registry URL is reachable.
func checkRegistryConnectivity(registryURL string) error {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(registryURL)
	if err != nil {
		return fmt.Errorf("failed to connect to registry URL '%s': %w", registryURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registry URL '%s' returned status: %s", registryURL, resp.Status)
	}

	return nil
}
