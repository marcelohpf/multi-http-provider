package multi_http_provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/traefik/genconf/dynamic"
)

type Endpoint struct {
	Endpoint string            `json:"endpoint,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
}

// Config the plugin configuration.
type Config struct {
	PollInterval string              `json:"pollInterval,omitempty"`
	PollTimeout  string              `json:"pollTimeout,omitempty"`
	EntryPoints  []string            `json:"entrypoints,omitempty"`
	Endpoints    map[string]Endpoint `json:"endpoints,omitempty"`
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{
		PollInterval: "15s",
		PollTimeout:  "10s",
		Endpoints:    map[string]Endpoint{},
		EntryPoints:  []string{},
	}
}

type endpoint struct {
	endpoint string
	headers  map[string]string
}

// Provider a simple provider plugin.
type Provider struct {
	name         string
	pollInterval time.Duration
	pollTimeout  time.Duration
	endpoints    map[string]endpoint
	entrypoints  map[string]bool
	cancel       func()
}

// New creates a new Provider plugin.
func New(ctx context.Context, config *Config, name string) (*Provider, error) {
	pi, err := time.ParseDuration(config.PollInterval)
	if err != nil {
		return nil, err
	}

	pt, err := time.ParseDuration(config.PollTimeout)
	if err != nil {
		return nil, err
	}

	endpoints := map[string]endpoint{}
	for k, v := range config.Endpoints {
		endpoints[k] = endpoint{
			endpoint: v.Endpoint,
			headers:  v.Headers,
		}
	}
	entrypoints := map[string]bool{}
	for _, entrypoint := range config.EntryPoints {
		entrypoints[entrypoint] = true
	}

	return &Provider{
		name:         name,
		pollInterval: pi,
		pollTimeout:  pt,
		endpoints:    endpoints,
		entrypoints:  entrypoints,
	}, nil
}

// Init the provider.
func (p *Provider) Init() error {
	if p.pollInterval <= 0 {
		return fmt.Errorf("poll interval must be greater than 0")
	}
	if p.pollTimeout <= 0 {
		return fmt.Errorf("poll timeout must be greater than 0")
	}
	if len(p.endpoints) <= 0 {
		return fmt.Errorf("must provide at least 1 endpoint")
	}
	if len(p.entrypoints) <= 0 {
		return fmt.Errorf("must specify at least one entrypoint")
	}
	return nil
}

// Provide creates and send dynamic configuration.
func (p *Provider) Provide(cfgChan chan<- json.Marshaler) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	go func() {
		defer func() {
			if err := recover(); err != nil {
				log.Print(err)
			}
		}()

		p.loadConfiguration(ctx, cfgChan)
	}()

	return nil
}

func (p *Provider) loadConfiguration(ctx context.Context, cfgChan chan<- json.Marshaler) {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			configs := map[string]*dynamic.Configuration{}
			for node, e := range p.endpoints {
				resp, err := http.Get(fmt.Sprintf("http://%s:5000/traefik/config", e.endpoint))
				if err != nil {
					log.Print("Error making request to %s:", e.endpoint, err)
					continue
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					log.Print("Error reading response body from %s:", e.endpoint, err)
					continue
				}
				var config dynamic.Configuration
				err = json.Unmarshal(body, &config)
				if err != nil {
					log.Print("Error decoding body from %s into dynamic configuration:", e.endpoint, err)
					continue
				}
				// https://pkg.go.dev/github.com/traefik/traefik/v3@v3.1.6/pkg/config/dynamic#Configuration

				toDelete := map[string]string{}
				toDeleteMiddlewares := map[string]bool{}
				for k, v := range config.HTTP.Routers {
					var entrypoints []string
					for _, e := range v.EntryPoints {
						if _, ok := p.entrypoints[e]; ok {
							entrypoints = append(entrypoints, e)
						}
					}
					for _, m := range v.Middlewares {
						if _, ok := toDeleteMiddlewares[m]; !ok {
							toDeleteMiddlewares[m] = true
						}
					}
					if len(entrypoints) == 0 {
						toDelete[k] = v.Service
					} else {
						for _, m := range v.Middlewares {
							toDeleteMiddlewares[m] = false
						}
					}
					v.EntryPoints = entrypoints
				}
				for k, v := range toDelete {
					delete(config.HTTP.Routers, k)
					delete(config.HTTP.Services, v)
				}
				for k, v := range toDeleteMiddlewares {
					if v {
						delete(config.HTTP.Middlewares, k)
					}
				}
				configs[node] = &config
			}
			config := mergeConfig(configs)
			cfgChan <- dynamic.JSONPayload{Configuration: config}
		case <-ctx.Done():
			return
		}
	}
}
func mergeConfig(configs map[string]*dynamic.Configuration) *dynamic.Configuration {
	newConfig := &dynamic.Configuration{
		HTTP: &dynamic.HTTPConfiguration{
			Routers:     map[string]*dynamic.Router{},
			Services:    map[string]*dynamic.Service{},
			Middlewares: map[string]*dynamic.Middleware{},
		},
	}
	for _, c := range configs {
		for name, m := range c.HTTP.Middlewares {
			if _, ok := newConfig.HTTP.Middlewares[name]; !ok {
				newConfig.HTTP.Middlewares[name] = m
			}
		}
		for name, m := range c.HTTP.Services {
			if _, ok := newConfig.HTTP.Services[name]; !ok {
				newConfig.HTTP.Services[name] = m
			}
		}
		for name, m := range c.HTTP.Routers {
			if _, ok := newConfig.HTTP.Routers[name]; !ok {
				newConfig.HTTP.Routers[name] = m
			}
		}
	}
	return newConfig
}

type ConfigMarshaler struct {
	config *dynamic.Configuration
}

func (c ConfigMarshaler) MarshalJSON() ([]byte, error) {
	if c.config != nil {
		return json.Marshal(c.config)
	}
	return nil, fmt.Errorf("unable to serialize configuration")
}

// Stop to stop the provider and the related go routines.
func (p *Provider) Stop() error {
	p.cancel()
	return nil
}
