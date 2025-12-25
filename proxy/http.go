package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/absmach/propeller/pkg/proplet"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const (
	tag  = "latest"
	size = 1024 * 1024
)

type HTTPProxyConfig struct {
	ChunkSize    int
	Authenticate bool
	Token        string
	Username     string
	Password     string
	RegistryURL  string
}

func (c *HTTPProxyConfig) FetchFromReg(ctx context.Context, containerPath string, chunkSize int) ([]proplet.ChunkPayload, error) {
	repo, err := remote.NewRepository(containerPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository for %s: %w", containerPath, err)
	}

	c.setupAuthentication(repo)

	manifest, err := c.fetchManifest(ctx, repo, containerPath)
	if err != nil {
		return nil, err
	}

	largestLayer, err := findLargestLayer(manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to find layer for %s: %w", containerPath, err)
	}

	log.Printf("Container size: %d bytes (%.2f MB)", largestLayer.Size, float64(largestLayer.Size)/size)

	layerReader, err := repo.Fetch(ctx, largestLayer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch layer for %s: %w", containerPath, err)
	}
	defer layerReader.Close()

	data, err := io.ReadAll(layerReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read layer for %s: %w", containerPath, err)
	}

	return createChunks(data, containerPath, chunkSize), nil
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

func createChunks(data []byte, containerPath string, chunkSize int) []proplet.ChunkPayload {
	dataSize := len(data)
	totalChunks := (dataSize + chunkSize - 1) / chunkSize

	chunks := make([]proplet.ChunkPayload, 0, totalChunks)
	for i := range make([]struct{}, totalChunks) {
		start := i * chunkSize
		end := min(start+chunkSize, dataSize)

		chunkData := data[start:end]
		log.Printf("Chunk %d size: %d bytes", i, len(chunkData))

		chunks = append(chunks, proplet.ChunkPayload{
			AppName:     containerPath,
			ChunkIdx:    i,
			TotalChunks: totalChunks,
			Data:        chunkData,
		})
	}

	return chunks
}
