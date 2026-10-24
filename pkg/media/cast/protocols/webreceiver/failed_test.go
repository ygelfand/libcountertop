package webreceiver

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

func TestAStreamTheServerClosedEndsAndABrokenOneErrs(t *testing.T) {
	r := New(
		cast.Env{Output: quietOutput{}, Video: func() playback.Target { return playback.Target{} }},
		Options{},
	)
	srv := httptest.NewServer(r.handler())
	defer srv.Close()
	c, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(srv.URL, "http")+"/media",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	reports := make(chan report, 8)
	go func() {
		for {
			var got report
			if c.ReadJSON(&got) != nil {
				return
			}
			reports <- got
		}
	}()
	await := func(fail func(), want string) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			fail()
			select {
			case got := <-reports:
				if got.Event != want {
					t.Fatalf("the page was told %q, want %q", got.Event, want)
				}
				return
			case <-time.After(20 * time.Millisecond):
			case <-deadline:
				t.Fatalf("the page was never told %q", want)
			}
		}
	}

	await(func() { remote{r}.Failed(io.ErrUnexpectedEOF) }, "ended")
	await(func() { remote{r}.Failed(errors.New("undecodable")) }, "error")
}
