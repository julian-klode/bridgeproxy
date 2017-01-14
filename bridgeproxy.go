// Package bridgeproxy relays with a remote TLS host via a HTTP proxy bridge.
package bridgeproxy

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

// Peer is a server we are connecting to. This can either be an
// intermediate http(s) proxy server or the final server we want
// to connect to.
type Peer struct {
	TLSConfig    *tls.Config // nil if unencrypted, valid config otherwise
	HostName     string      // The hostname to connect to
	Port         int         // The port to connect to on the hostname
	ConnectExtra string      // Extra headers to send after the CONNECT line
}

func forward(src io.ReadCloser, dst io.WriteCloser) {
	if _, err := io.Copy(dst, src); err != nil {
		fmt.Println("Could not forward:", err)
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

func handleRequest(client net.Conn, peers []Peer) {
	var connection net.Conn
	var err error

	for i, peer := range peers {
		// The first peer has to be dialed, others happen via connect
		if i == 0 {
			connection, err = net.Dial("tcp", fmt.Sprintf("%s:%d", peer.HostName, peer.Port))
			if err != nil {
				fmt.Println("ERROR: Could not connect", err)
				return
			}
		} else {
			fmt.Fprintf(connection, "CONNECT %s:%d HTTP/1.0\r\n%s\r\n\r\n", peer.HostName, peer.Port, peers[i-1].ConnectExtra)

			line, err := readLine(io.LimitReader(connection, 1024))
			if err != nil {
				fmt.Println("Could not read:", err)
				return
			}
			if !strings.HasPrefix(line, "HTTP/1.0 200") && !strings.HasPrefix(line, "HTTP/1.1 200") {
				client.Write([]byte("HTTP/1.0 502 Bad Gateway\r\nConnection: close\r\n\r\n"))
				client.Write([]byte("Server error: " + line))
				return
			}
			if line, err = readLine(connection); err != nil {
				fmt.Println("Invalid second response line:", line)
				return
			}
		}

		if peer.TLSConfig != nil {
			connection = tls.Client(connection, peer.TLSConfig)
		}
	}

	// Forward traffic between the client connected to us and the remote proxy
	go forward(client, connection)
	go forward(connection, client)
}

// Serve serves the specified configuration, forwarding any packets from the
// local address given in listenAdress to the last peer specified in peers via
// any peers before specified before it.
func Serve(listenAdress string, peers []Peer) {
	// Listen for incoming connections.
	l, err := net.Listen("tcp", listenAdress)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		os.Exit(1)
	}
	// Close the listener when the application closes.
	defer l.Close()
	fmt.Println("Listening on", listenAdress)
	fmt.Println("- Forwarding requests to", peers[len(peers)-1], "via", peers[0:len(peers)-1])
	for {
		// Listen for an incoming connection.
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}
		// Handle connections in a new goroutine.
		go handleRequest(conn, peers)
	}
}
