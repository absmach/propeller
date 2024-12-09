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
	Root         string `env:"ROOT"         envDefault:""`
	RegURL       string `env:"REG_URL"      envDefault:""`
	UserRepo     string `env:"USER_REPO"    envDefault:""`
	Authenticate bool   `env:"AUTHENTICATE" envDefault:"false"`
	Username     string `env:"USERNAME"     envDefault:""`
	Password     string `env:"PASSWORD"     envDefault:""`
}

func FetchFromOCI(ctx context.Context) ([]byte, error) {
	config := Config{}
	if err := env.ParseWithOptions(&config, env.Options{Prefix: envPrefix}); err != nil {
		return nil, err
	}
	store, err := oci.New(config.Root)
	if err != nil {
		return nil, err
	}

	repo, err := remote.NewRepository(config.RegURL + config.UserRepo)
	if err != nil {
		return nil, err
	}

	if config.Authenticate {
		repo.Client = &auth.Client{
			Client: retry.DefaultClient,
			Cache:  auth.NewCache(),
			Credential: auth.StaticCredential(config.RegURL, auth.Credential{
				Username: config.Username,
				Password: config.Password,
			}),
		}
	}

	manifestDescriptor, err := oras.Copy(ctx, repo, tag, store, tag, oras.DefaultCopyOptions)
	if err != nil {
		return nil, err
	}

	return manifestDescriptor.Data, nil
}
