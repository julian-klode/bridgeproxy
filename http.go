package bridgeproxy

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"
)

// httpProxyClient implements a http.Handler for proxying requests
type httpProxyClient struct {
	http.Client
	peers []Peer
}

// ServeHTTP serves proxy requests
func (client *httpProxyClient) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// authenticate
	for k, v := range client.peers[len(client.peers)-1].ConnectExtra {
		r.Header.Add(k, v)
	}
	// net/http.Client does not handle the CONNECT stuff that well below, so
	// let us go a more direct route here - this could be used for the other
	// methods as well, but that would prevent reusing connections to the
	// proxy.
	if r.Method == "CONNECT" {
		log.Println("Dialing for CONNECT to", r.URL)
		remote, err := DialProxy(client.peers)
		if err != nil {
			log.Println(err)
			w.WriteHeader(500)
			w.Write([]byte(err.Error()))
			return
		}

		if err = r.WriteProxy(remote); err != nil {
			log.Println(err)
			w.WriteHeader(500)
			w.Write([]byte(err.Error()))
			return
		}

		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			log.Println(err)
			w.WriteHeader(500)
			w.Write([]byte(err.Error()))
			return
		}

		go copyAndClose(conn, remote)
		copyAndClose(remote, conn)
		return
	}

	// The wonderful GET/POST/PUT/HEAD wonderland - this actually uses the
	// http library with a fake dial function that allows us to cache and
	// reuse connections to the proxy, speeding up the whole affair quite
	// a bit if you have to do TLS handshakes.
	if r.URL.Scheme == "" {
		r.URL.Scheme = "http"
		r.URL.Host = r.Host
	}
	r.RequestURI = ""
	res, err := client.Do(r)
	if err != nil {
		log.Println("Could not do", r, "-", err)
		w.WriteHeader(500)
		return
	}

	for k, vs := range res.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	io.Copy(w, res.Body)
	res.Body.Close()
}

// HTTPProxyHandler constructs a handler for http.ListenAndServe()
// that proxies HTTP requests via the configured proxies. It supports
// not only HTTP proxy requests, but also normal HTTP/1.1 requests with a
// Host header - thus enabling the use as a transparent proxy.
func HTTPProxyHandler(peers []Peer) http.Handler {
	host := fmt.Sprintf("%s:%d", peers[len(peers)-1].HostName, peers[len(peers)-1].Port)
	transport := http.Transport{
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     5 * time.Minute,

		Proxy: func(r *http.Request) (*url.URL, error) {
			return &url.URL{Scheme: "http",
				Host: host}, nil
		},
		Dial: func(network, addr string) (net.Conn, error) {
			if addr != host {
				return nil, fmt.Errorf("Target is not the proxy host: %s is not %s", addr, host)
			}
			log.Println("Dial called for", addr)
			c, err := DialProxy(peers)
			if err != nil {
				if c != nil {
					c.Close()
				}
				return nil, err
			}
			return c, nil
		},
	}
	client := http.Client{
		Transport: &transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &httpProxyClient{client, peers}
}
