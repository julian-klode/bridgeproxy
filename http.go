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

// httpProxyHandler implements a http.Handler for proxying requests
type httpProxyHandler struct {
	client http.Client
	peers  []Peer
}

// serveHTTPConnect serves proxy requests for the CONNECT method. It does not
// print errors, but rather returns them for your proxy handler to handle.
func (proxy *httpProxyHandler) serveHTTPConnect(w http.ResponseWriter, r *http.Request) error {
	log.Println("Dialing for CONNECT to", r.URL)
	remote, err := DialProxy(proxy.peers)
	if err != nil {
		return err
	}

	if err = r.WriteProxy(remote); err != nil {
		return err
	}

	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return err
	}

	go copyAndClose(conn, remote)
	copyAndClose(remote, conn)
	return nil
}

// ServeHTTP serves proxy requests
func (proxy *httpProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// authenticate
	for k, vs := range proxy.peers[len(proxy.peers)-1].ConnectExtra {
		for _, v := range vs {
			r.Header.Add(k, v)
		}
	}
	// net/http.Client does not handle the CONNECT stuff that well below, so
	// let us go a more direct route here - this could be used for the other
	// methods as well, but that would prevent reusing connections to the
	// proxy.
	if r.Method == "CONNECT" {
		if err := proxy.serveHTTPConnect(w, r); err != nil {
			log.Println(err)
			w.WriteHeader(500)
			w.Write([]byte(err.Error()))
		}
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
	res, err := proxy.client.Do(r)
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

	return &httpProxyHandler{client, peers}
}
