package bridgeproxy

import "fmt"
import "net"
import "os"
import "io"
import "crypto/tls"

type Configuration struct {
	Local      string
	Bridge     string
	RemoteName string
	RemotePort string
}

func forward(src net.Conn, dst net.Conn) {
	defer src.Close()
	defer dst.Close()
	for {
		n, err := io.Copy(dst, src)
		if n == 0 {
			break
		}
		if err != nil {
			fmt.Println("Could not forward:", err)
			break
		}
		fmt.Println("Forwarded", n, "bytes from", src, "to", dst)
	}
}

func handleRequest(browser net.Conn, item Configuration) {
	fmt.Println("handleRequest")
	conn, err := net.Dial("tcp", item.Bridge)
	if err != nil {
		fmt.Println("ERROR: Could not connect", err)
		return
	}
	fmt.Fprintf(conn, "CONNECT %s:%s HTTP/1.0\r\n\r\n\r\n", item.RemoteName, item.RemotePort)

	// Read the "HTTP/1.0 200 Connection established" and the 2 \r\n
	_, err = io.ReadFull(conn, make([]byte, 39))
	if err != nil {
		fmt.Println("Could not read:", err)
		return
	}

	// We now have access to the TLS connection.
	client := tls.Client(conn, &tls.Config{ServerName: item.RemoteName})

	// Forward traffic between the client connected to us and the remote proxy
	go forward(browser, client)
	go forward(client, browser)
}

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
