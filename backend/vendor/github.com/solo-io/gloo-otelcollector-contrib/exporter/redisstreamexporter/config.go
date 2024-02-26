package redisstreamexporter

import (
	"errors"
	"time"

	"go.opentelemetry.io/collector/exporter/exporterhelper"

	"github.com/solo-io/gloo-otelcollector-contrib/exporter/redisstreamexporter/version"
)

type Config struct {
	exporterhelper.QueueSettings   `mapstructure:"sending_queue"`
	exporterhelper.RetrySettings   `mapstructure:"retry_on_failure"`
	exporterhelper.TimeoutSettings `mapstructure:",squash"` // squash ensures fields are correctly decoded in embedded struct

	// Redis server address.
	Endpoint string `mapstructure:"endpoint"`

	// Redis stream name (default "otel-stream-<version>").
	Stream string `mapstructure:"stream"`

	// TopicAttributes is a list of attributes to use as the stream key (default []).
	TopicAttributes TopicAttributes `mapstructure:"stream_attributes"`

	// Expire sets the time until the redis stream has expired and is removed from redis (default 30m).
	Expire time.Duration `mapstructure:"expire"`
	// Max number of log entries to have in the stream (default 128).
	MaxEntries int64 `mapstructure:"max_entries"`
	// TODO(SirNexus): flesh out auth config options once config libary is enabled.
}

type TopicAttributes struct {
	ResourceAttributes []string `mapstructure:"resource_attributes"`
	LogAttributes      []string `mapstructure:"log_attributes"`
}

func (cfg *Config) Validate() error {
	if cfg.Endpoint == "" {
		return errors.New("endpoint must be set")
	}

	return nil
}

const (
	DefaultMaxEntries = 128
	DefaultExpire     = 30 * time.Minute
)

var DefaultStream = "otel-stream-" + version.Version
