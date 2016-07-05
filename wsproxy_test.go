package wsproxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/net/websocket"
)

type Message struct {
	Foo string `json:"foo,omitempty"`
}

func TestRead(t *testing.T) {
	exp := []Message{
		{Foo: "bar"},
		{Foo: "baz"},
	}

	ts, wg := serve(Config{}, func(w http.ResponseWriter, r *http.Request) {
		bw := bufio.NewWriter(w)

		for _, e := range exp {
			require.NoError(t, write(bw, &e))
		}
	})
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close()

	for _, e := range exp {
		m := Message{}
		err := websocket.JSON.Receive(ws, &m)
		if err == io.EOF {
			break
		}

		if assert.NoError(t, err) {
			assert.Equal(t, e, m)
		}
	}

	wg.Wait()
}

func TestWrite(t *testing.T) {
	exp := []Message{
		{Foo: "bar"},
		{Foo: "baz"},
	}

	ts, wg := serve(Config{}, func(w http.ResponseWriter, r *http.Request) {
		br := bufio.NewReader(r.Body)

		for _, e := range exp {
			var m Message
			if assert.NoError(t, read(br, &m)) {
				assert.Equal(t, e, m)
			}
		}
	})
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close()

	for _, e := range exp {
		websocket.JSON.Send(ws, &e)
	}

	wg.Wait()
}

func TestPlain(t *testing.T) {
	ts, wg := serve(Config{}, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello World!")
	})
	defer ts.Close()

	r, err := http.Get(ts.URL)
	require.NoError(t, err, "Failed to request data from server.")
	defer r.Body.Close()

	b, err := ioutil.ReadAll(r.Body)
	if assert.NoError(t, err) {
		assert.Equal(t, "Hello World!", string(b))
	}

	wg.Wait()
}

func TestReadToken(t *testing.T) {
	c := Config{ReadToken: true}
	ts, wg := serve(c, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, r.Header.Get("Authorization"), "Bearer dummy token")
	})
	defer ts.Close()

	ws := dial(t, ts)
	assert.NoError(t, websocket.Message.Send(ws, "dummy token"))
	defer ws.Close()

	wg.Wait()
}

func TestRewriteMethod(t *testing.T) {
	c := Config{RewriteMethod: "POST"}
	ts, wg := serve(c, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		b, err := ioutil.ReadAll(r.Body)
		defer r.Body.Close()

		if assert.NoError(t, err) {
			assert.Equal(t, "Hello World!\n", string(b))
		}
	})
	defer ts.Close()

	ws := dial(t, ts)
	assert.NoError(t, websocket.Message.Send(ws, "Hello World!"))
	ws.Close()

	wg.Wait()
}

func serve(c Config, h func(http.ResponseWriter, *http.Request)) (*httptest.Server, *sync.WaitGroup) {
	wg := &sync.WaitGroup{}
	wg.Add(1)
	f := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		h(w, r)
	})
	s := httptest.NewServer(New(c, f))
	return s, wg
}

func dial(t *testing.T, ts *httptest.Server) *websocket.Conn {
	ws, err := websocket.Dial(strings.Replace(ts.URL, "http://", "ws://", 1), "", ts.URL)
	require.NoError(t, err, "Failed to establish websocket connection.")
	return ws
}

func read(br *bufio.Reader, m *Message) error {
	b, err := br.ReadBytes('\n')
	if err != nil {
		return err
	}
	return json.Unmarshal(b, m)
}

func write(bw *bufio.Writer, m *Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	bw.Write(b)
	bw.WriteRune('\n')
	return bw.Flush()
}
