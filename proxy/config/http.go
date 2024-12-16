package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const (
	tag       = "latest"
	size      = 1024 * 1024
	chunkSize = 512000
)

type ChunkPayload struct {
	AppName     string `json:"app_name"`
	ChunkIdx    int    `json:"chunk_idx"`
	TotalChunks int    `json:"total_chunks"`
	Data        []byte `json:"data"`
}

type HTTPProxyConfig struct {
	RegistryURL  string `env:"REGISTRY_URL" envDefault:"localhost:5000"`
	Authenticate bool   `env:"AUTHENTICATE" envDefault:"false"`
	Token        string `env:"PAT"          envDefault:""`
	Username     string `env:"USERNAME"     envDefault:""`
	Password     string `env:"PASSWORD"     envDefault:""`
}

func (c *HTTPProxyConfig) Validate() error {
	if c.RegistryURL == "" {
		return errors.New("broker_url is required")
	}
	if _, err := url.Parse(c.RegistryURL); err != nil {
		return fmt.Errorf("broker_url is not a valid URL: %w", err)
	}

	if c.Authenticate {
		hasToken := c.Token != ""
		hasCredentials := c.Username != "" && c.Password != ""

		if !hasToken && !hasCredentials {
			return errors.New("either PAT or username/password must be provided when authentication is enabled")
		}

		if hasToken && c.Username == "" {
			return errors.New("username is required when using PAT authentication")
		}
	}

	return nil
}

func (c *HTTPProxyConfig) setupAuthentication(repo *remote.Repository) {
	if !c.Authenticate {
		return
	}

	var cred auth.Credential
	if c.Username != "" && c.Password != "" {
		cred = auth.Credential{
			Username: c.Username,
			Password: c.Password,
		}
	} else if c.Token != "" {
		cred = auth.Credential{
			Username:    c.Username,
			AccessToken: c.Token,
		}
	}

	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.NewCache(),
		Credential: auth.StaticCredential(c.RegistryURL, cred),
	}
}

func (c *HTTPProxyConfig) fetchManifest(ctx context.Context, repo *remote.Repository, containerName string) (*ocispec.Manifest, error) {
	descriptor, err := repo.Resolve(ctx, tag)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve manifest for %s: %w", containerName, err)
	}

	reader, err := repo.Fetch(ctx, descriptor)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest for %s: %w", containerName, err)
	}
	defer reader.Close()

	manifestData, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest for %s: %w", containerName, err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest for %s: %w", containerName, err)
	}

	return &manifest, nil
}

func findLargestLayer(manifest *ocispec.Manifest) (ocispec.Descriptor, error) {
	var largestLayer ocispec.Descriptor
	var maxSize int64

	for _, layer := range manifest.Layers {
		if layer.Size > maxSize {
			maxSize = layer.Size
			largestLayer = layer
		}
	}

	if largestLayer.Size == 0 {
		return ocispec.Descriptor{}, errors.New("no valid layers found in manifest")
	}

	return largestLayer, nil
}

func createChunks(data []byte, containerName string) []ChunkPayload {
	dataSize := len(data)
	totalChunks := (dataSize + chunkSize - 1) / chunkSize

	// log.Printf("Total data size: %d bytes (%.2f MB)", dataSize, float64(dataSize)/size)
	// log.Printf("Chunk size: %d bytes (500 KB)", chunkSize)
	// log.Printf("Total chunks: %d", totalChunks)

	chunks := make([]ChunkPayload, 0, totalChunks)
	for i := range make([]struct{}, totalChunks) {
		start := i * chunkSize
		end := start + chunkSize
		if end > dataSize {
			end = dataSize
		}

		chunkData := data[start:end]
		log.Printf("Chunk %d size: %d bytes", i, len(chunkData))

		chunks = append(chunks, ChunkPayload{
			AppName:     containerName,
			ChunkIdx:    i,
			TotalChunks: totalChunks,
			Data:        chunkData,
		})
	}

	return chunks
}

func (c *HTTPProxyConfig) FetchFromReg(ctx context.Context, containerName string) ([]ChunkPayload, error) {
	fullPath := fmt.Sprintf("%s/%s", c.RegistryURL, containerName)

	repo, err := remote.NewRepository(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository for %s: %w", containerName, err)
	}

	c.setupAuthentication(repo)

	manifest, err := c.fetchManifest(ctx, repo, containerName)
	if err != nil {
		return nil, err
	}

	largestLayer, err := findLargestLayer(manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to find layer for %s: %w", containerName, err)
	}

	log.Printf("Container size: %d bytes (%.2f MB)", largestLayer.Size, float64(largestLayer.Size)/size)

	layerReader, err := repo.Fetch(ctx, largestLayer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch layer for %s: %w", containerName, err)
	}
	defer layerReader.Close()

	data, err := io.ReadAll(layerReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read layer for %s: %w", containerName, err)
	}

	return createChunks(data, containerName), nil
}
