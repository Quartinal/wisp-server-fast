package main

import (
	"net/http"
	"net/url"
    "syscall/js"

	wisp "github.com/Quartinal/wisp-server-go/http/js"
	connection "github.com/Quartinal/wisp-server-go/connection/js"
)

func main() {
	c := make(chan struct{})
	js.Global().Set("handleWebSocket", js.FuncOf(handleWebSocketWrapper))
	<-c
}

func handleWebSocketWrapper(this js.Value, args []js.Value) interface{} {
	if len(args) != 1 {
		return "Invalid number of arguments"
	}

	jsWsConn := args[0]

	// Create a custom ResponseWriter and Request
	w := &responseWriterWrapper{}
	r := &http.Request{
		RemoteAddr: jsWsConn.Get("socket").Get("remoteAddress").String(),
		URL: &url.URL{
			Path: jsWsConn.Get("url").String(),
		},
	}

	// Create a websocketWrapper
	ws := &connection.WebSocketReadWriter{JsWsConn: jsWsConn}

	// Call the original handleWebSocket function
	go wisp.HandleWebSocketJS(w, r, ws)

	return nil
}

type responseWriterWrapper struct {
	header http.Header
	status int
	body   []byte
}

func (w *responseWriterWrapper) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *responseWriterWrapper) Write(data []byte) (int, error) {
	w.body = append(w.body, data...)
	return len(data), nil
}

func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.status = statusCode
}