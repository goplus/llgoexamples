package main

import (
	"fmt"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/net"
	"github.com/goplus/llgoexamples/c/libuv"
)

var DEFAULT_PORT = 8080
var DEFAULT_BACKLOG = 128

var (
	Req *libuv.Write
	Buf libuv.Buf
)

func main() {
	// Initialize the default event loop
	var loop = libuv.DefaultLoop()

	// Initialize a TCP server
	server := libuv.NewTcp()
	libuv.InitTcp(loop, server)

	// Set up the address to bind the server to
	var addr net.SockaddrIn
	libuv.Ip4Addr("0.0.0.0", DEFAULT_PORT, &addr)
	fmt.Printf("Listening on 0.0.0.0:%d\n", DEFAULT_PORT)

	// Bind the server to the specified address and port
	server.Bind((*net.SockAddr)(unsafe.Pointer(&addr)), 0)
	res := (*libuv.Stream)(unsafe.Pointer(server)).Listen(DEFAULT_BACKLOG, OnNewConnection)
	if res != 0 {
		fmt.Printf("Listen error: %s\n", libuv.Strerror(res))
		return
	}

	// Start listening for incoming connections
	loop.Run(libuv.RUN_DEFAULT)
}

func FreeWriteReq() {
	// Free the buffer base.
	c.Free(unsafe.Pointer(Buf.Buf.Base))
}

func AllocBuffer(handle *libuv.Handle, suggestedSize uintptr, buf *libuv.Buf) {
	// Allocate memory for the buffer based on the suggested size.
	buf.Buf.Base = (*c.Char)(c.Malloc(suggestedSize))
	buf.Buf.Len = suggestedSize
}

func EchoWrite(req *libuv.Write, status c.Int) {
	if status != 0 {
		fmt.Printf("Write error: %s\n", libuv.Strerror(int(status)))
	}
	FreeWriteReq()
}

func EchoRead(client *libuv.Stream, nread c.Long, buf *libuv.Buf) {
	if nread > 0 {
		// Initialize the buffer with the data read.
		Buf = libuv.InitBufRaw(buf.Base, c.Uint(nread))
		// Write the data back to the client.
		Req = libuv.NewWrite()
		Req.Write(client, &Buf, 1, EchoWrite)
		return
	}
	if nread < 0 {
		// Handle read errors and EOF.
		if nread != c.Long(libuv.EOF) {
			fmt.Printf("Read error: %s\n", libuv.Strerror(int(nread)))
		}
		(*libuv.Handle)(unsafe.Pointer(client)).Close(nil)
	}
	// Free the buffer if it's no longer needed.
	if buf.Base != nil {
		c.Free(unsafe.Pointer(buf.Base))
	}
}

func OnNewConnection(server *libuv.Stream, status c.Int) {
	if status < 0 {
		fmt.Printf("New connection error: %s\n", libuv.Strerror(int(status)))
		return
	}

	// Allocate memory for a new client.
	client := libuv.NewTcp()

	if client == nil {
		fmt.Printf("Failed to allocate memory for client\n")
		return
	}

	// Initialize the client TCP handle.
	if libuv.InitTcp(libuv.DefaultLoop(), client) < 0 {
		fmt.Printf("Failed to initialize client\n")
		c.Free(unsafe.Pointer(client))
		return
	}

	// Accept the new connection and start reading data.
	if server.Accept((*libuv.Stream)(unsafe.Pointer(client))) == 0 {
		(*libuv.Stream)(unsafe.Pointer(client)).StartRead(AllocBuffer, EchoRead)
	} else {
		(*libuv.Handle)(unsafe.Pointer(client)).Close(nil)
	}
}
