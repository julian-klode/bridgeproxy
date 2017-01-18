package bridgeproxy

import (
	"bytes"
	"fmt"
	"log"
	"net"

	"github.com/inconshreveable/go-vhost"
)

// hijackTLSRequest handles a client connecting via TLS, connects to the
// specified peers and then issues a CONNECT to the host requested by the
// client to port 443.
func hijackTLSRequest(client net.Conn, peers []Peer) {
	tlsClientConn, err := vhost.TLS(client)
	defer func() {
		if client != nil {
			client.Close()
		}
	}()
	if err != nil {
		log.Println("Error accepting new connection:", err)
		return
	}
	if tlsClientConn.Host() == "" {
		log.Println("Cannot support non-SNI enabled clients")
		return
	}

	proxy, err := DialProxy(peers)
	if err != nil {
		log.Println("Cannot dial proxy:", err)
		return
	}

	proxy, err = doHTTPConnect(proxy, Peer{HostName: tlsClientConn.Host(), Port: 443}, peers[len(peers)-1])
	if err != nil {
		log.Println("Cannot do final HTTP connect:", err)
		return
	}

	client = nil
	go copyAndClose(tlsClientConn, proxy)
	go copyAndClose(proxy, tlsClientConn)
}

// ListenTLS listens on the given address for TLS connections with
// Server Name Indication (SNI) and proxies them via CONNECT through
// the given peers.
func ListenTLS(laddr string, peers []Peer) {
	ln, err := net.Listen("tcp", laddr)
	if err != nil {
		log.Fatalf("Error listening for TLS connections - %v", err)
	}
	var buffer bytes.Buffer
	for _, peer := range peers {
		fmt.Fprintf(&buffer, " → %s:%d", peer.HostName, peer.Port)
	}
	log.Printf("Forwarding TLS: %s%s\n", laddr, buffer.String())
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Println("Error accepting new connection:", err)
			continue
		}
		go hijackTLSRequest(c, peers)
	}
}
