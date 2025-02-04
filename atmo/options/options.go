package options

import (
	"context"

	"github.com/pkg/errors"
	"github.com/sethvargo/go-envconfig"

	"github.com/suborbital/vektor/vlog"
)

const atmoEnvPrefix = "ATMO"

// Options defines options for Atmo.
type Options struct {
	Logger           *vlog.Logger
	BundlePath       string `env:"ATMO_BUNDLE_PATH"`
	RunSchedules     *bool  `env:"ATMO_RUN_SCHEDULES,default=true"`
	Headless         *bool  `env:"ATMO_HEADLESS,default=false"`
	Wait             *bool  `env:"ATMO_WAIT,default=false"`
	ControlPlane     string `env:"ATMO_CONTROL_PLANE"`
	EnvironmentToken string `env:"ATMO_ENV_TOKEN"`
	StaticPeers      string `env:"ATMO_PEERS"`
	AppName          string `env:"ATMO_APP_NAME,default=Atmo"`
	Domain           string `env:"ATMO_DOMAIN"`
	HTTPPort         int    `env:"ATMO_HTTP_PORT,default=8080"`
	TLSPort          int    `env:"ATMO_TLS_PORT,default=443"`
	Proxy            bool
	TracerConfig     TracerConfig `env:",prefix=ATMO_TRACER_"`
}

// TracerConfig holds values specific to setting up the tracer. It's only used in proxy mode. All configuration options
// have a prefix of ATMO_TRACER_ specified in the parent Options struct.
type TracerConfig struct {
	TracerType      string           `env:"TYPE,default=none"`
	ServiceName     string           `env:"SERVICENAME,default=atmo"`
	Probability     float64          `env:"PROBABILITY,default=0.5"`
	Collector       *CollectorConfig `env:",prefix=COLLECTOR_,noinit"`
	HoneycombConfig *HoneycombConfig `env:",prefix=HONEYCOMB_,noinit"`
}

// CollectorConfig holds config values specific to the collector tracer exporter running locally / within your cluster.
// All the configuration values here have a prefix of ATMO_TRACER_COLLECTOR_, specified in the top level Options struct,
// and the parent TracerConfig struct.
type CollectorConfig struct {
	Endpoint string `env:"ENDPOINT"`
}

// HoneycombConfig holds config values specific to the honeycomb tracer exporter. All the configuration values here have
// a prefix of ATMO_TRACER_HONEYCOMB_, specified in the top level Options struct, and the parent TracerConfig struct.
type HoneycombConfig struct {
	Endpoint string `env:"ENDPOINT"`
	APIKey   string `env:"APIKEY"`
	Dataset  string `env:"DATASET"`
}

// Modifier defines options for Atmo.
type Modifier func(*Options)

func NewWithModifiers(mods ...Modifier) *Options {
	opts := &Options{}

	for _, mod := range mods {
		mod(opts)
	}

	opts.finalize(atmoEnvPrefix)

	return opts
}

// UseLogger sets the logger to be used.
func UseLogger(logger *vlog.Logger) Modifier {
	return func(opts *Options) {
		opts.Logger = logger
	}
}

// UseBundlePath sets the bundle path to be used.
func UseBundlePath(path string) Modifier {
	return func(opts *Options) {
		opts.BundlePath = path
	}
}

// ShouldRunHeadless sets wether Atmo should operate in 'headless' mode.
func ShouldRunHeadless(headless bool) Modifier {
	return func(opts *Options) {
		// only set the pointer if the value is true.
		if headless {
			opts.Headless = &headless
		}
	}
}

// ShouldWait sets wether Atmo should wait for a bundle to become available on disk.
func ShouldWait(wait bool) Modifier {
	return func(opts *Options) {
		// only set the pointer if the value is true.
		if wait {
			opts.Wait = &wait
		}
	}
}

// AppName sets the app name to be used.
func AppName(name string) Modifier {
	return func(opts *Options) {
		opts.AppName = name
	}
}

// Domain sets the domain to be used.
func Domain(domain string) Modifier {
	return func(opts *Options) {
		opts.Domain = domain
	}
}

// HTTPPort sets the http port to be used.
func HTTPPort(port int) Modifier {
	return func(opts *Options) {
		opts.HTTPPort = port
	}
}

// TLSPort sets the tls port to be used.
func TLSPort(port int) Modifier {
	return func(opts *Options) {
		opts.TLSPort = port
	}
}

// finalize "locks in" the options by overriding any existing options with the version from the environment, and setting the default logger if needed.
func (o *Options) finalize(prefix string) {
	if o.Logger == nil {
		o.Logger = vlog.Default(vlog.EnvPrefix(prefix))
	}

	envOpts := Options{}
	if err := envconfig.Process(context.Background(), &envOpts); err != nil {
		o.Logger.Error(errors.Wrap(err, "failed to Process environment config"))
		return
	}

	o.ControlPlane = envOpts.ControlPlane

	// set RunSchedules if it was not passed as a flag.
	if o.RunSchedules == nil {
		if envOpts.RunSchedules != nil {
			o.RunSchedules = envOpts.RunSchedules
		}
	}

	// set Wait if it was not passed as a flag
	// if Wait is unset but ControlPlane IS set,
	// Wait is implied to be true.
	if o.Wait == nil {
		if o.ControlPlane != "" {
			wait := true
			o.Wait = &wait
		} else if envOpts.Wait != nil {
			o.Wait = envOpts.Wait
		}
	}

	// set Headless if it was not passed as a flag.
	if o.Headless == nil {
		if envOpts.Headless != nil {
			o.Headless = envOpts.Headless
		}
	}

	// set AppName if it was not passed as a flag.
	if o.AppName == "" {
		o.AppName = envOpts.AppName
	}

	// set Domain if it was not passed as a flag.
	if o.Domain == "" {
		o.Domain = envOpts.Domain
	}

	// set HTTPPort if it was not passed as a flag.
	if o.HTTPPort == 0 {
		o.HTTPPort = envOpts.HTTPPort
	}

	// set TLSPort if it was not passed as a flag.
	if o.TLSPort == 0 {
		o.TLSPort = envOpts.TLSPort
	}

	o.EnvironmentToken = ""
	o.TracerConfig = TracerConfig{}
	o.StaticPeers = envOpts.StaticPeers

	// compile-time decision about enabling proxy mode.
	o.Proxy = proxyEnabled()

	// only set the env token and tracer config in config if we're in proxy mode.
	if o.Proxy {
		o.EnvironmentToken = envOpts.EnvironmentToken
		o.TracerConfig = envOpts.TracerConfig
	}
}
