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

	ts := serve(Config{}, func(w http.ResponseWriter, r *http.Request) {
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
}

func TestWrite(t *testing.T) {
	exp := []Message{
		{Foo: "bar"},
		{Foo: "baz"},
	}

	wg := sync.WaitGroup{}
	ts := serve(Config{}, func(w http.ResponseWriter, r *http.Request) {
		wg.Add(1)
		defer wg.Done()

		b, err := ioutil.ReadAll(r.Body)
		assert.NoError(t, err)
		fmt.Println("b", string(b))
		assert.FailNow(t, "KURWA")

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
	ts := serve(Config{}, func(w http.ResponseWriter, r *http.Request) {
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
}

func TestReadToken(t *testing.T) {
	c := Config{ReadToken: true}
	ts := serve(c, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, r.Header.Get("Authorization"), "Bearer dummy token")
	})
	defer ts.Close()

	ws := dial(t, ts)
	assert.NoError(t, websocket.Message.Send(ws, "dummy token"))
	defer ws.Close()
}

func TestRewriteMethod(t *testing.T) {
	c := Config{RewriteMethod: "POST"}
	ts := serve(c, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		b, err := ioutil.ReadAll(r.Body)
		defer r.Body.Close()

		if assert.NoError(t, err) {
			assert.Equal(t, "Hello World!", string(b))
		}
	})
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close()
	assert.NoError(t, websocket.Message.Send(ws, "Hello World!"))
}

func serve(c Config, h func(http.ResponseWriter, *http.Request)) *httptest.Server {
	return httptest.NewServer(New(c, http.HandlerFunc(h)))
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
	fmt.Println("b", string(b))
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
