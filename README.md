# bridgeproxy - Decrypting bridge to remote HTTPS proxy

Represent a remote HTTPS proxy as a local HTTP proxy, while connecting to
the remote HTTPS proxy via a bridge HTTP proxy.

## Example use

```go
package main

import "github.com/julian-klode/bridgeproxy"

func main() {
	bridgeproxy.Serve(bridgeproxy.Configuration{
		Local:      "localhost:9090",
		Bridge:     "local-squid-proxy:3128",
		RemoteName: "remote-host.example.com",
		RemotePort: "443"})
}
```
