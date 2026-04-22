package observability

import "time"

// Config holds unified observability configuration for all Propeller services.
type Config struct {
	// Metrics settings
	MetricsEnabled bool `env:"PROPELLER_METRICS_ENABLED" envDefault:"true"`
	MetricsPort    int  `env:"PROPELLER_METRICS_PORT"    envDefault:"9090"`

	// Logging settings
	LogExportEnabled bool   `env:"PROPELLER_LOG_EXPORT_ENABLED" envDefault:"false"`
	LogLevel         string `env:"PROPELLER_LOG_LEVEL"          envDefault:"info"`

	// Service identification
	ServiceName    string `env:"OTEL_SERVICE_NAME"`
	ServiceVersion string `env:"OTEL_SERVICE_VERSION" envDefault:"dev"`

	// Export settings
	ExportTimeout  time.Duration `env:"PROPELLER_EXPORT_TIMEOUT"  envDefault:"10s"`
	ExportInterval time.Duration `env:"PROPELLER_EXPORT_INTERVAL" envDefault:"15s"`

	// Retry settings for graceful degradation
	MaxRetries    int           `env:"PROPELLER_OTEL_MAX_RETRIES"     envDefault:"3"`
	RetryInterval time.Duration `env:"PROPELLER_OTEL_RETRY_INTERVAL"  envDefault:"5s"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MetricsEnabled:   true,
		MetricsPort:      9090,
		LogExportEnabled: false,
		LogLevel:         "info",
		ServiceVersion:   "dev",
		ExportTimeout:    10 * time.Second,
		ExportInterval:   15 * time.Second,
		MaxRetries:       3,
		RetryInterval:    5 * time.Second,
	}
}

// WithServiceName sets the service name for the config.
func (c Config) WithServiceName(name string) Config {
	c.ServiceName = name

	return c
}
