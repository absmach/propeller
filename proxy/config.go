package proxy

import "github.com/caarlos0/env/v11"

type Config struct {
	Address      string `env:"ADDRESS"      envDefault:""`
	PathPrefix   string `env:"PATH_PREFIX"  envDefault:"/"`
	Target       string `env:"TARGET"       envDefault:""`
	Root         string `env:"ROOT"         envDefault:""`
	RegURL       string `env:"REG_URL"      envDefault:""`
	UserRepo     string `env:"USER_REPO"    envDefault:""`
	Authenticate bool   `env:"AUTHENTICATE" envDefault:"false"`
	Username     string `env:"USERNAME"     envDefault:""`
	Password     string `env:"PASSWORD"     envDefault:""`
}

func NewConfig(opts env.Options) (Config, error) {
	c := Config{}
	if err := env.ParseWithOptions(&c, opts); err != nil {
		return Config{}, err
	}

	return c, nil
}
