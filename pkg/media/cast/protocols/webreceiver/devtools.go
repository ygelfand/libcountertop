package webreceiver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

type devtools struct {
	conn *websocket.Conn

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

func dialPage(ctx context.Context, port int) (*devtools, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"http://127.0.0.1:"+strconv.Itoa(port)+"/json/list",
		nil,
	)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webreceiver: devtools: %w", err)
	}
	defer resp.Body.Close()
	var targets []struct {
		Type string `json:"type"`
		URL  string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, fmt.Errorf("webreceiver: devtools: %w", err)
	}
	for _, t := range targets {
		if t.Type != "page" {
			continue
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, t.URL, nil)
		if err != nil {
			return nil, fmt.Errorf("webreceiver: devtools: %w", err)
		}
		return &devtools{conn: conn}, nil
	}
	return nil, errors.New("webreceiver: devtools: no page to drive")
}

func (d *devtools) call(method string, params any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.next++
	return d.conn.WriteJSON(map[string]any{"id": d.next, "method": method, "params": params})
}

func (d *devtools) request(
	ctx context.Context,
	method string,
	params any,
) (json.RawMessage, error) {
	got := make(chan reply, 1)
	d.mu.Lock()
	d.next++
	id := d.next
	if d.replies == nil {
		d.replies = map[int64]chan reply{}
	}
	d.replies[id] = got
	err := d.conn.WriteJSON(map[string]any{"id": id, "method": method, "params": params})
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.replies, id)
		d.mu.Unlock()
	}()
	if err != nil {
		return nil, fmt.Errorf("webreceiver: devtools: %w", err)
	}
	select {
	case r := <-got:
		return r.result, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *devtools) answer(id int64, result json.RawMessage, failed *failure) {
	d.mu.Lock()
	got := d.replies[id]
	d.mu.Unlock()
	if got == nil {
		return
	}
	r := reply{result: result}
	if failed != nil {
		r.err = fmt.Errorf("webreceiver: devtools: %s", failed.Message)
	}
	got <- r
}

func (d *devtools) size(w, h int) error {
	return d.call("Emulation.setDeviceMetricsOverride", map[string]any{
		"width": w, "height": h, "deviceScaleFactor": 1, "mobile": false,
	})
}

func (d *devtools) navigate(url string) error {
	return d.call("Page.navigate", map[string]any{"url": url})
}

func (d *devtools) close() { _ = d.conn.Close() }

func (d *devtools) read() error {
	for {
		var m struct {
			ID     int64           `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *failure        `json:"error"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := d.conn.ReadJSON(&m); err != nil {
			return fmt.Errorf("webreceiver: devtools: %w", err)
		}
		switch m.Method {
		case "":
			d.answer(m.ID, m.Result, m.Error)
		case "Runtime.consoleAPICalled":
			var p struct {
				Type string `json:"type"`
				Args []struct {
					Value       any    `json:"value"`
					Description string `json:"description"`
				} `json:"args"`
			}
			if json.Unmarshal(m.Params, &p) != nil {
				continue
			}
			parts := make([]string, 0, len(p.Args))
			for _, a := range p.Args {
				if a.Value != nil {
					parts = append(parts, fmt.Sprint(a.Value))
				} else {
					parts = append(parts, a.Description)
				}
			}
			slog.Debug("cast page console", "level", p.Type, "text", strings.Join(parts, " "))
		case "Runtime.exceptionThrown":
			var p struct {
				Details struct {
					Text      string `json:"text"`
					Exception struct {
						Description string `json:"description"`
					} `json:"exception"`
				} `json:"exceptionDetails"`
			}
			if json.Unmarshal(m.Params, &p) == nil {
				slog.Debug("cast page exception", "text", p.Details.Text,
					"exception", p.Details.Exception.Description)
			}
		}
	}
}
