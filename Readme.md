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
	//By default there are not Limits.
	bl := bandwidth.NewListener(context.Background(), ln)

	writeBytesPerSecond, readBytesPerSecond := bandwidth.NewConfig(1000), bandwidth.NewConfig(2000)
	bl.SetGlobalLimits(writeBytesPerSecond, readBytesPerSecond)
	bl.SetConnLimits(writeBytesPerSecond, readBytesPerSecond)
	
	err = server.Serve(bl)
	if err != nil {
		// handle error.
	}
}
```

# Run unit tests

```shell
go test ./... -v
```
or
```shell
go test ./... -v -short
```