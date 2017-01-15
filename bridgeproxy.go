/*
Package bridgeproxy provides a framework for writing proxies that connect
through one or more upstream proxies (called Peer below).

There are two main entry functions that can be used:

1. Serve() provides access to the last peer under the given address. This can
be used to implement a TLS-decrypting proxy server: Just specify a HTTPS
proxy as the last peer, and it will be available as an HTTP proxy on the
chosen address.

2. ListenTLS() provides a way to HIJACK TLS requests: A client connecting to
the specified address will be connected via the peers to the address it
indicates via SNI (Server Name Indication) in the TLS handshake

TODO: Implement a transparent HTTP proxy.
*/
package bridgeproxy

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
)

// Peer is a server we are connecting to. This can either be an
// intermediate http(s) proxy server or the final server we want
// to connect to.
type Peer struct {
	TLSConfig    *tls.Config       // nil if unencrypted, valid config otherwise
	HostName     string            // The hostname to connect to
	Port         int               // The port to connect to on the hostname
	ConnectExtra map[string]string // Extra headers to send after the CONNECT line
}

// copyAndClose copies bytes from src to dst and closes both afterwards
func copyAndClose(dst io.WriteCloser, src io.ReadCloser) {
	if _, err := io.Copy(dst, src); err != nil {
		log.Println("Could not forward:", err)
	}
	src.Close()
	dst.Close()
}

// readLine reads a network line one byte at a time. We need to read unbuffered
// as we might later turn the connection after a 200 response for a CONNECT
// into a tls connection, for which we need a net.Conn.
func readLine(src io.Reader) (string, error) {
	line := make([]byte, 0, 64)
	length := 0
	for length < 2 || line[length-2] != '\r' || line[length-1] != '\n' {
		line = append(line, 0)
		if _, err := io.ReadFull(src, line[length:]); err != nil {
			return "", err
		}
		length++
	}
	return string(line[:length]), nil
}

// doHTTPConnect issues an HTTP/1.0 CONNECT request on a connection. It
// always returns a connection, but may also return an error.
//
// The parameter peer describes the peer we want to connect to
// The parameter activePeer is the latest peer we connected to in this chain
func doHTTPConnect(connection net.Conn, peer Peer, activePeer Peer) (net.Conn, error) {

	if _, err := fmt.Fprintf(connection, "CONNECT %s:%d HTTP/1.0\r\n", peer.HostName, peer.Port); err != nil {
		return connection, fmt.Errorf("failure writing CONNECT to %s: %s", peer.HostName, err.Error())
	}

	for k, v := range activePeer.ConnectExtra {
		if _, err := fmt.Fprintf(connection, "%s: %s\r\n", k, v); err != nil {
			return connection, fmt.Errorf("failure writing CONNECT header to %s: %s", peer.HostName, err.Error())
		}
	}

	if _, err := fmt.Fprintf(connection, "\r\n"); err != nil {
		return connection, fmt.Errorf("failure writing CONNECT end of headers to %s: %s", peer.HostName, err.Error())
	}

	line, err := readLine(io.LimitReader(connection, 1024))
	if err != nil {
		return connection, fmt.Errorf("could not CONNECT to %s: %s\r\n", peer.HostName, err.Error())
	}
	if !strings.HasPrefix(line, "HTTP/1.0 200") && !strings.HasPrefix(line, "HTTP/1.1 200") {
		return connection, fmt.Errorf("could not CONNECT to %s: %s", peer.HostName, line)
	}
	if _, err = readLine(connection); err != nil {
		return connection, fmt.Errorf("could not CONNECT to %s: Missing second line", peer.HostName)
	}
	return connection, nil
}

// DialProxy dials a proxy using the given slice of peers. It returns a
// network connection and error. Even if an error is returned, there may
// be a network connection that needs to be closed.
func DialProxy(peers []Peer) (net.Conn, error) {
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
