// Package bridgeproxy relays with a remote TLS host via a HTTP proxy bridge.
package bridgeproxy

import "fmt"
import "net"
import "os"
import "io"
import "crypto/tls"

// Configuration configures a server to be served using Serve() below.
// The bridge must be an HTTP proxy.
// The remote must be a TLS server that the bridge allows CONNECT-ing to
type Configuration struct {
	Local      string // The local address to listen to (host:port)
	Bridge     string // The bridge to connect to (host:port)
	RemoteName string // Host name of the final destination
	RemotePort string // Port of the final destination
}

func forward(src net.Conn, dst net.Conn) {
	if _, err := io.Copy(dst, src); err != nil {
		fmt.Println("Could not forward:", err)
	}
	src.Close()
	dst.Close()
}

func handleRequest(client net.Conn, item Configuration) {
	var bridgename string = item.Bridge
	if item.Bridge == "" {
		bridgename = item.RemoteName + ":" + item.RemotePort
	}

	bridge, err := net.Dial("tcp", bridgename)
	if err != nil {
		fmt.Println("ERROR: Could not connect", err)
		return
	}
	if item.Bridge != "" {
		fmt.Fprintf(bridge, "CONNECT %s:%s HTTP/1.0\r\n\r\n\r\n", item.RemoteName, item.RemotePort)

		// Read the "HTTP/1.0 200 Connection established" and the 2 \r\n
		_, err = io.ReadFull(bridge, make([]byte, 39))
		if err != nil {
			fmt.Println("Could not read:", err)
			return
		}
	}

	// We now have access to the TLS connection.
	remote := tls.Client(bridge, &tls.Config{ServerName: item.RemoteName})

	// Forward traffic between the client connected to us and the remote proxy
	go forward(client, remote)
	go forward(remote, client)
}

// Serve serves the specified configuration, forwarding any packets between
// the local socket and the remote one, bridged via an HTTP proxy.
// It returns nothing.
func Serve(item Configuration) {
	// Listen for incoming connections.
	l, err := net.Listen("tcp", item.Local)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		os.Exit(1)
	}
	// Close the listener when the application closes.
	defer l.Close()
	fmt.Println("Listening on", item.Local)
	fmt.Println("- Forwarding requests to", item.RemoteName, "port", item.RemotePort, "via", item.Bridge)
	for {
		// Listen for an incoming connection.
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}
		// Handle connections in a new goroutine.
		go handleRequest(conn, item)
	}
}
