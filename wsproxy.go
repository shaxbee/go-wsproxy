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

// WebSocketProxy adds websocket capability to JSON Streaming HTTP/2 services
type WebSocketProxy struct {
	c Config
	h http.Handler
}

// Config contains parameters for WebSocketProxy
type Config struct {
	// Expect first message to contain OAuth token.
	// Provided token will be forwarder to handler in Authorization header.
	ReadToken bool
	// Rewrite GET method used in websocket connection to provided value.
	// Ignored if empty.
	RewriteMethod string
}

// New creates instance of WebSocketProxy wrapping given http.Handler
// Wrapped handler will proxy underlying request through websocket.
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

	orp, iwp := io.Pipe()
	defer iwp.Close()

	irp, owp := io.Pipe()
	defer owp.Close()

	var method string
	if wp.c.RewriteMethod != "" {
		method = wp.c.RewriteMethod
	} else {
		method = req.Method
	}

	nreq, err := http.NewRequest(method, req.URL.String(), irp)
	if err != nil {
		glog.Errorf("shaxbee/go-wsproxy: Error creating request: %s", err)
	}
	if wp.c.ReadToken {
		var tok string
		if err := websocket.Message.Receive(ws, &tok); err != nil {
			return
		}
		nreq.Header.Set("Authorization", "Bearer "+tok)
	}
	nreq.Cancel = ctx.Done()

	glog.V(2).Infof("shaxbee/go-wsproxy: Forwarding websocket to %s %s", method, req.URL.String())
	go wp.h.ServeHTTP(respForwarder(iwp), nreq)

	go listenWrite(ctx, ws, bufio.NewReader(orp))
	listenRead(ctx, ws, bufio.NewWriter(owp))
}

func listenRead(ctx context.Context, ws *websocket.Conn, w *bufio.Writer) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			var m string
			err := websocket.Message.Receive(ws, &m)
			if err == io.EOF {
				return
			} else if err != nil {
				glog.Errorf("shaxbee/go-wsproxy: Error while reading from websocket: %s", err)
				return
			}

			w.WriteString(m)
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
			s, err := r.ReadString('\n')
			if err == io.EOF {
				return
			} else if err != nil {
				glog.Errorf("shaxbee/go-wsproxy: Error while reading response: %s", err)
				return
			}

			if err := websocket.Message.Send(ws, s); err != nil {
				glog.Errorf("shaxbee/go-wsproxy: Error while writing to websocket: %s", err)
				return
			}
		}
	}

}

func respForwarder(w *io.PipeWriter) http.ResponseWriter {
	return &responseForwarder{w, make(http.Header)}
}

type responseForwarder struct {
	*io.PipeWriter
	h http.Header
}

func (rf *responseForwarder) Header() http.Header {
	return rf.h
}

func (rf *responseForwarder) WriteHeader(int) {

}

func (rf *responseForwarder) Flush() {

}
