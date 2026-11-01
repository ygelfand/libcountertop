package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/gorilla/websocket"
)

type conn struct {
	ws    *websocket.Conn
	event func(session, method string, params json.RawMessage)

	mu      sync.Mutex
	next    int64
	replies map[int64]chan reply
}

type reply struct {
	result json.RawMessage
	err    error
}

type failure struct {
	Message string `json:"message"`
}

type message struct {
	ID        int64           `json:"id,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    any             `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *failure        `json:"error,omitempty"`
}

func dial(ctx context.Context, port int) (*conn, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"http://127.0.0.1:"+strconv.Itoa(port)+"/json/version",
		nil,
	)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("engine: devtools: %w", err)
	}
	defer resp.Body.Close()
	var v struct {
		URL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, fmt.Errorf("engine: devtools: %w", err)
	}
	ws, _, err := websocket.DefaultDialer.DialContext(ctx, v.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: devtools: %w", err)
	}
	return &conn{ws: ws}, nil
}

func (c *conn) call(session, method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	return c.write(message{ID: c.next, SessionID: session, Method: method, Params: params})
}

func (c *conn) request(
	ctx context.Context,
	session, method string,
	params any,
) (json.RawMessage, error) {
	got := make(chan reply, 1)
	c.mu.Lock()
	c.next++
	id := c.next
	if c.replies == nil {
		c.replies = map[int64]chan reply{}
	}
	c.replies[id] = got
	err := c.write(message{ID: id, SessionID: session, Method: method, Params: params})
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.replies, id)
		c.mu.Unlock()
	}()
	if err != nil {
		return nil, err
	}
	select {
	case r := <-got:
		return r.result, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *conn) write(m message) error {
	if m.Params == nil {
		m.Params = struct{}{}
	}
	if err := c.ws.WriteJSON(m); err != nil {
		return fmt.Errorf("engine: devtools: %w", err)
	}
	return nil
}

func (c *conn) answer(id int64, result json.RawMessage, failed *failure) {
	c.mu.Lock()
	got := c.replies[id]
	c.mu.Unlock()
	if got == nil {
		return
	}
	r := reply{result: result}
	if failed != nil {
		r.err = fmt.Errorf("engine: devtools: %s", failed.Message)
	}
	got <- r
}

func (c *conn) read() error {
	for {
		var m struct {
			ID        int64           `json:"id"`
			SessionID string          `json:"sessionId"`
			Method    string          `json:"method"`
			Params    json.RawMessage `json:"params"`
			Result    json.RawMessage `json:"result"`
			Error     *failure        `json:"error"`
		}
		if err := c.ws.ReadJSON(&m); err != nil {
			return fmt.Errorf("engine: devtools: %w", err)
		}
		if m.Method == "" {
			c.answer(m.ID, m.Result, m.Error)
			continue
		}
		if c.event != nil {
			c.event(m.SessionID, m.Method, m.Params)
		}
	}
}

func (c *conn) close() { _ = c.ws.Close() }
