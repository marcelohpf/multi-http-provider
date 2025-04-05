# Multi HTTP Provider

This is a provider plugin for traefik that allows you to use multiple http sources.

# Configuration

```
# Static configuration
experimental:
  plugins:
    multi-http-provider:
      moduleName: github.com/marcelohpf/multi-http-provider
      version: vX.Y.Z
providers:
  plugin:
    multi-http-provider:
      pollInterval: 60s
      pollTimeout: 30s
      endpoints:
        server1:
            endpoint: 10.0.1.2
        server2:
            endpoint: 192.168.0.3
            headers:
                X-Custom-Header: value
```
