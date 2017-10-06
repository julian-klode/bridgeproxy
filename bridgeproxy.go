/*
Package bridgeproxy provides a framework for writing proxies that connect
through one or more upstream proxies (called Peer below).

There are three main entry functions that can be used:

1. Serve() provides access to the last peer under the given address. This can
be used to implement a TLS-decrypting proxy server: Just specify a HTTPS
proxy as the last peer, and it will be available as an HTTP proxy on the
chosen address.

2. ListenTLS() provides a way to HIJACK TLS requests: A client connecting to
the specified address will be connected via the peers to the address it
indicates via SNI (Server Name Indication) in the TLS handshake

3. HTTPProxyHandler() constructs a http.Handler that can be used with
http.ListenAndServe() to create an HTTP proxy that accepts CONNECT, GET,
HEAD, and possibly other types of requests.
*/
package bridgeproxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Peer is a server we are connecting to. This can either be an
// intermediate http(s) proxy server or the final server we want
// to connect to.
type Peer struct {
	TLSConfig    *tls.Config         // nil if unencrypted, valid config otherwise
	HostName     string              // The hostname to connect to
	Port         int                 // The port to connect to on the hostname
	ConnectExtra map[string][]string // Extra headers to send after the CONNECT line
}

// copyAndClose copies bytes from src to dst and closes both afterwards
func copyAndClose(dst io.WriteCloser, src io.ReadCloser) {
	if _, err := io.Copy(dst, src); err != nil {
		log.Println("Could not forward:", err)
	}
	src.Close()
	dst.Close()
}

// httpConnectResponseConn wraps a connection with a reader so we can read
// the response code first and then read the rest from the reader.
type httpConnectResponseConn struct {
	net.Conn
	io.ReadCloser
}

// Read should read from the reader, not the connection
func (conn *httpConnectResponseConn) Read(b []byte) (int, error) {
	return conn.ReadCloser.Read(b)
}

// Close should close the body not the connection
func (conn *httpConnectResponseConn) Close() error {
	return conn.ReadCloser.Close()
}

// doHTTPConnect issues an HTTP CONNECT request on a connection. It
// always returns a connection, but may also return an error.
//
// The parameter peer describes the peer we want to connect to
// The parameter activePeer is the latest peer we connected to in this chain
func doHTTPConnect(connection net.Conn, peer Peer, activePeer Peer) (net.Conn, error) {
	req := http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Path: fmt.Sprintf("%s:%d", peer.HostName, peer.Port)},
		Header: http.Header(activePeer.ConnectExtra),
	}

	if err := req.Write(connection); err != nil {
		return connection, fmt.Errorf("connecting to %s: %s", peer.HostName, err.Error())
	}

	res, err := http.ReadResponse(bufio.NewReader(connection), &req)
	switch {
	case err != nil:
		return connection, fmt.Errorf("reading response: connecting to %s: %s", peer.HostName, err.Error())
	case res.StatusCode != 200:
		return connection, fmt.Errorf("invalid status code: connecting to %s: %d", peer.HostName, res.StatusCode)
	}

	return &httpConnectResponseConn{connection, res.Body}, nil
}

// DialProxyInternal dials a proxy using the given slice of peers. It returns a
// network connection and error. Even if an error is returned, there may
// be a network connection that needs to be closed.
func DialProxyInternal(peers []Peer) (net.Conn, error) {
	var connection net.Conn
	var err error
	for i, peer := range peers {
		// The first peer has to be dialed, others happen via connect
		if i == 0 {
			connection, err = net.Dial("tcp", fmt.Sprintf("%s:%d", peer.HostName, peer.Port))
		} else {
			connection, err = doHTTPConnect(connection, peer, peers[i-1])
		}
		if err != nil {
			return connection, err
		}

		if peer.TLSConfig != nil {
			tlsConnection := tls.Client(connection, peer.TLSConfig)
			if err := tlsConnection.Handshake(); err != nil {
				return connection, fmt.Errorf("handshake with %s failed: %s", peer.HostName, err)
			}
			connection = tlsConnection
		}
	}
	return connection, nil
}

type connResult struct {
	c net.Conn
	e error
}

var tcpConnections = make(map[string]chan connResult)

// DialProxy is a buffered version of DialProxyInternal(). It keeps a channel for a given list of peers
// and generates new connections in a background goroutine, thus removing the overhead for establishing
// new connections for all except the first one (and occassional timed out ones).
func DialProxy(peers []Peer) (net.Conn, error) {
	a := time.Now()
	peersAsString := ""
	for _, peer := range peers {
		peersAsString += fmt.Sprintf("%s:%d/", peer.HostName, peer.Port)
	}
	chn, ok := tcpConnections[peersAsString]
	if !ok {
		chn = make(chan connResult)
		tcpConnections[peersAsString] = chn

		go func() {
			for {
				a := time.Now()
				conn, err := DialProxyInternal(peers)
				log.Printf("Established %s in the background in %s", peersAsString, time.Now().Sub(a))
				chn <- connResult{conn, err}
			}
		}()
	}

	for {
		res := <-chn
		// Discard closed connections
		if _, err := res.c.Read(make([]byte, 0, 0)); err != nil {
			log.Printf("Discarding: %s", err)
			continue
		}
		if res.e != nil {
			return nil, res.e
		}
		log.Printf("Fully established %s in %s", peersAsString, time.Now().Sub(a))
		return res.c, nil
	}
}
