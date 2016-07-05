package wsproxy

import (
	"bufio"
	"io"
	"net/http"
	"strings"

	"github.com/golang/glog"

	"golang.org/x/net/context"
	"golang.org/x/net/websocket"
)

// WebSocketProxy adds WebSocket capability to JSON Streaming HTTP/2 services
type WebSocketProxy struct {
	c Config
	h http.Handler
}

// Config contains parameters for WebSocketProxy
type Config struct {
	ReadToken     bool
	RewriteMethod string
}

// New creates instance of WebSocketProxy wrapping given http.Handler
// Wrapped handler will proxy underlying request through WebSocket.
// If upgrade to websocket is not requested handler will be invoked directly.
func New(c Config, h http.Handler) *WebSocketProxy {
	return &WebSocketProxy{c, h}
}

func (wp *WebSocketProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.ToLower(r.Header.Get("Upgrade")) != "websocket" {
		wp.h.ServeHTTP(w, r)
		return
	}

	wsh := websocket.Handler(func(ws *websocket.Conn) { wp.proxy(r, ws) })
	wsh.ServeHTTP(w, r)
}

func (wp *WebSocketProxy) proxy(req *http.Request, ws *websocket.Conn) {
	defer ws.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var header http.Header
	if wp.c.ReadToken {
		var tok string
		if err := websocket.Message.Receive(ws, &tok); err != nil {
			return
		}
		header = http.Header{"Authorization": {"Bearer " + tok}}
	}

	var method string
	if wp.c.RewriteMethod != "" {
		method = wp.c.RewriteMethod
	} else {
		method = req.Method
	}

	orp, iwp := io.Pipe()
	or := bufio.NewReader(orp)

	irp, owp := io.Pipe()
	ow := bufio.NewWriter(owp)

	go wp.h.ServeHTTP(respForwarder(iwp), &http.Request{
		Method:        method,
		URL:           req.URL,
		Proto:         "HTTP/2",
		ProtoMajor:    2,
		ProtoMinor:    0,
		Header:        header,
		Body:          irp,
		ContentLength: -1,
		Host:          req.Host,
		RemoteAddr:    req.RemoteAddr,
		Cancel:        req.Cancel,
	})

	go listenRead(ctx, ws, ow)
	listenWrite(ctx, ws, or)
}

func listenRead(ctx context.Context, ws *websocket.Conn, w *bufio.Writer) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, err := w.ReadFrom(ws)
			if err == io.EOF {
				return
			} else if err != nil {
				glog.Errorf("shaxbee/go-wsproxy: Error while reading from websocket: %s", err)
				return
			}

			w.WriteRune('\n')
			if err := w.Flush(); err != nil {
				glog.Errorf("shaxbee/go-wsproxy: Error while writing request: %s", err)
				return
			}
		}
	}
}

func listenWrite(ctx context.Context, ws *websocket.Conn, r *bufio.Reader) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			b, err := r.ReadBytes('\n')
			if err == io.EOF {
				return
			} else if err != nil {
				glog.Errorf("shaxbee/go-wsproxy: Error while reading response: %s", err)
				return
			}

			if err := websocket.Message.Send(ws, b); err != nil {
				glog.Errorf("shaxbee/go-wsproxy: Error while writing to websocket: %s", err)
				return
			}
		}
	}

}

func respForwarder(w io.Writer) http.ResponseWriter {
	return &responseForwarder{w, make(http.Header)}
}

type responseForwarder struct {
	io.Writer
	h http.Header
}

func (rf *responseForwarder) Header() http.Header {
	return rf.h
}

func (rf *responseForwarder) WriteHeader(int) {

}
