package main

import (
	"fmt"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/os"
	"github.com/goplus/llgoexamples/c/libuv"
)

const BUFFER_SIZE = 1024

var (
	loop     *libuv.Loop
	openReq  libuv.Fs
	closeReq libuv.Fs

	buffer [BUFFER_SIZE]c.Char
	iov    libuv.Buf
	file   int
)

func main() {
	//fmt.Printf("libuv version: %d\n", libuv.Version())
	loop = libuv.DefaultLoop()
	openReq = libuv.NewFs()
	closeReq = libuv.NewFs()

	// Open the file
	result := openReq.Open(loop, "example.txt", os.O_RDONLY, 0, onOpen)
	if result != 0 {
		fmt.Printf("Error in Open: %s (code: %d)\n", libuv.Strerror(result), result)
		return
	}

	// Run the loop
	res := loop.Run(libuv.RUN_DEFAULT)
	if res != 0 {
		fmt.Printf("Error in Run: %s\n", libuv.Strerror(res))
		loop.Stop()
	}
	fmt.Printf("loop.Run(libuv.RUN_DEFAULT) = %d\n", res)

	// Cleanup
	defer cleanup()
}

func onOpen(req *libuv.Fs) {
	fmt.Println("onOpen")
	// Check for errors
	if req.GetResult() < 0 {
		fmt.Printf("Error opening file: %s\n", libuv.Strerror(req.GetResult()))
		loop.Stop()
		return
	}

	// Store the file descriptor
	file = req.GetResult()

	// Init buffer
	iov = libuv.InitBuf(buffer[:])

	// Read the file
	readFile()
}

func readFile() {
	// Initialize the request every time
	readReq := libuv.NewFs()

	// Read the file
	readRes := readReq.Read(loop, file, iov, 1, -1, onRead)
	if readRes != 0 {
		fmt.Printf("Error in FsRead: %s (code: %d)\n", libuv.Strerror(readReq.GetResult()), readRes)
		loop.Stop()
		return
	}
}

func onRead(req *libuv.Fs) {
	fmt.Println("onRead")
	// Cleanup the request
	defer req.Cleanup()
	// Check for errors
	if req.GetResult() < 0 {
		fmt.Printf("Read error: %s\n", libuv.Strerror(req.GetResult()))
	} else if req.GetResult() == 0 {
		// Close the file
		closeRes := closeReq.Close(loop, openReq.GetResult(), onClose)
		if closeRes != 0 {
			fmt.Printf("Error in FsClose: %s (code: %d)\n", libuv.Strerror(req.GetResult()), closeRes)
			loop.Stop()
			return
		}
	} else {
		// Print the content
		fmt.Printf("Read %d bytes\n", req.GetResult())
		fmt.Printf("Read content: %.*s\n",
			req.GetResult(),
			(*[BUFFER_SIZE]byte)(unsafe.Pointer(&buffer[0]))[:])
		// Read the file again
		readFile()
	}
}

func onClose(req *libuv.Fs) {
	fmt.Println("onClose")
	// Check for errors
	if req.GetResult() < 0 {
		fmt.Printf("Error closing file: %s\n", libuv.Strerror(req.GetResult()))
	} else {
		fmt.Printf("\nFile closed successfully.\n")
	}
}

func cleanup() {
	// Cleanup the requests
	openReq.Cleanup()
	closeReq.Cleanup()
	// Close the loop
	result := loop.Close()
	if result != 0 {
		fmt.Printf("Error in LoopClose: %s\n", libuv.Strerror(result))
	}
}
