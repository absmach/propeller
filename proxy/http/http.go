package http

import (
	"context"

	"github.com/caarlos0/env/v11"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const tag = "latest"

var envPrefix = "ORAS_"

type Config struct {
	Root         string `env:"ROOT"         envDefault:"/tmp/oras_oci_folder"`
	Authenticate bool   `env:"AUTHENTICATE" envDefault:"false"`
	Username     string `env:"USERNAME"     envDefault:""`
	Password     string `env:"PASSWORD"     envDefault:""`
}

func Init() (*Config, error) {
	config := Config{}
	if err := env.ParseWithOptions(&config, env.Options{Prefix: envPrefix}); err != nil {
		return nil, err
	}

	return &config, nil
}

func (c *Config) FetchFromReg(ctx context.Context, regURL, filePath string) ([]byte, error) {
	store, err := oci.New(c.Root)
	if err != nil {
		return nil, err
	}

	repo, err := remote.NewRepository(regURL + filePath)
	if err != nil {
		return nil, err
	}

	if c.Authenticate {
		repo.Client = &auth.Client{
			Client: retry.DefaultClient,
			Cache:  auth.NewCache(),
			Credential: auth.StaticCredential(regURL, auth.Credential{
				Username: c.Username,
				Password: c.Password,
			}),
		}
	}

	manifestDescriptor, err := oras.Copy(ctx, repo, tag, store, tag, oras.DefaultCopyOptions)
	if err != nil {
		return nil, err
	}

	reader, err := store.Fetch(ctx, manifestDescriptor)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return manifestDescriptor.Data, nil
}
