The QOS (Quality of service) package provides:
- bandwidth rate limiting.


Rate bandwidth allows for limiting bytes which can be sent or retrieved within some time.

Example usage for HTTP server:
```go
package main

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"

	"github.com/informalict/qos/bandwidth"
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/helloworld", func(writer http.ResponseWriter, request *http.Request) {
		io.WriteString(writer, "Hello world!")
	})

	server := &http.Server{Addr: ":8080", Handler: mux}
	server.ListenAndServe()

	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		log.Fatal("can not create tcp listner")
	}
	//By default, there are no Limits.
	bl := bandwidth.NewListener(context.Background(), ln)

	bytesPerSecond := 1000
	// burst describes how many bytes can be performed within one call of rate limiter.
	// usually it is the same value as bytesPerSecond.
	burst := 1000
	writeBytesPerSecond := bandwidth.NewConfig(bytesPerSecond, burst)
	readBytesPerSecond := bandwidth.NewConfig(bytesPerSecond, burst)
	bl.SetGlobalLimits(writeBytesPerSecond, readBytesPerSecond)
	bl.SetConnLimits(writeBytesPerSecond, readBytesPerSecond)
	
	err = server.Serve(bl)
	if err != nil {
		// handle error.
	}
}
```

# Run unit tests

Run all tests:
```shell
go test ./... -v
```
or run only shorter test:
```shell
go test ./... -v -short
```