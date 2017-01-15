// Functions for TCP proxies. Redirect any TCP packet to the last peer.

package bridgeproxy

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

// writeHTTPResponse writes a response to w with the given code and body,
// the latter being defined by a printf() format strings and arguments.
func writeHTTPResponse(w io.Writer, code int, format string, printf ...interface{}) {
	msg := fmt.Sprintf(format, printf...)
	fmt.Fprintf(w, "HTTP/1.0 %d %s\r\n", code, http.StatusText(code))
	fmt.Fprintf(w, "Content-Type: text/plain\r\n")
	fmt.Fprintf(w, "Content-Length: %d\r\n", len(msg))
	fmt.Fprintf(w, "\r\n")
	fmt.Fprintf(w, "%s", msg)
}

// handleRequest handles a request by calling dialProxy() and then forwarding
func handleRequest(client io.ReadWriteCloser, peers []Peer) {
	remote, err := DialProxy(peers)
	if err != nil {
		log.Println("Error:", strings.TrimSpace(err.Error()))
		writeHTTPResponse(client, 502, "Error: %s", err.Error())
		if remote != nil {
			remote.Close()
		}
		client.Close()
		return
	}

	go copyAndClose(client, remote)
	go copyAndClose(remote, client)
}

// Serve serves the specified configuration, forwarding any packets from the
// local address given in listenAdress to the last peer specified in peers via
// any peers before specified before it.
func Serve(listenAdress string, peers []Peer) {
	// Listen for incoming connections.
	l, err := net.Listen("tcp", listenAdress)
	if err != nil {
		log.Println("Error listening:", err.Error())
		os.Exit(1)
	}
	// Close the listener when the application closes.
	defer l.Close()
	log.Println("Listening on", listenAdress)
	log.Println("- Forwarding requests to", peers[len(peers)-1], "via", peers[0:len(peers)-1])
	for {
		// Listen for an incoming connection.
		conn, err := l.Accept()
		if err != nil {
			log.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}
		// Handle connections in a new goroutine.
		go handleRequest(conn, peers)
	}
}
