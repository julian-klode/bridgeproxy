# bridgeproxy - Decrypting bridge to remote HTTPS proxy

Represent a remote HTTPS proxy as a local HTTP proxy, while connecting to
the remote HTTPS proxy via recursive bridge HTTP proxies.

[![GoDoc](https://godoc.org/github.com/julian-klode/bridgeproxy?status.svg)](https://godoc.org/github.com/julian-klode/bridgeproxy) [![Go Report Card](https://goreportcard.com/badge/github.com/julian-klode/bridgeproxy)](https://goreportcard.com/report/github.com/julian-klode/bridgeproxy)

## Documentation
See http://godoc.org/github.com/julian-klode/bridgeproxy for documentation.

## Example use

For example, the following connects to second-proxy by first connecting
to first-proxy. The second proxy is tls encrypted.

```go
package main

import (
	"crypto/tls"
	"github.com/julian-klode/bridgeproxy"
)

func main() {

	bridgeproxy.Serve(
		"localhost:9091",
		[]bridgeproxy.Peer{
			{
				HostName: "first-proxy.example.com",
				Port:     3128,
			},
			{
				TLSConfig:    &tls.Config{InsecureSkipVerify: true},
				HostName:     "second-proxy.example.com",
				Port:         443,
			},
		})
}
```
