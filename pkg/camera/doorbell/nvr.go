package doorbell

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	nvrWait  = 15 * time.Second
	maxReply = 1 << 20
)

var ErrNVRLogin = errors.New("doorbell: the NVR refused the user or password")

type Channel struct {
	Number int
	Name   string
	Online bool
}

type nvrReply struct {
	Cmd   string          `json:"cmd"`
	Code  int             `json:"code"`
	Value json.RawMessage `json:"value"`
	Error struct {
		Detail  string `json:"detail"`
		RspCode int    `json:"rspCode"`
	} `json:"error"`
}

func Channels(ctx context.Context, host, user, password string) ([]Channel, error) {
	client := &http.Client{
		Timeout:   nvrWait,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	var lastErr error
	for _, scheme := range []string{"https", "http"} {
		base := scheme + "://" + host + "/api.cgi"
		token, err := nvrLogin(ctx, client, base, user, password)
		if err != nil {
			if errors.Is(err, ErrNVRLogin) {
				return nil, err
			}
			lastErr = err
			continue
		}
		defer nvrLogout(client, base, token)
		return nvrChannels(ctx, client, base, token)
	}
	return nil, errors.Join(ErrUnreachable, lastErr)
}

func nvrLogin(ctx context.Context, c *http.Client, base, user, password string) (string, error) {
	var reply struct {
		Token struct {
			Name string `json:"name"`
		} `json:"Token"`
	}
	body := []map[string]any{{
		"cmd": "Login",
		"param": map[string]any{
			"User": map[string]string{"Version": "0", "userName": user, "password": password},
		},
	}}
	if err := nvrCall(ctx, c, base+"?cmd=Login", body, &reply); err != nil {
		return "", err
	}
	if reply.Token.Name == "" {
		return "", ErrNVRLogin
	}
	return reply.Token.Name, nil
}

func nvrChannels(ctx context.Context, c *http.Client, base, token string) ([]Channel, error) {
	var reply struct {
		Status []struct {
			Channel int    `json:"channel"`
			Name    string `json:"name"`
			Online  int    `json:"online"`
		} `json:"status"`
	}
	body := []map[string]any{{"cmd": "GetChannelstatus"}}
	if err := nvrCall(
		ctx,
		c,
		base+"?cmd=GetChannelstatus&token="+url.QueryEscape(token),
		body,
		&reply,
	); err != nil {
		return nil, err
	}
	out := make([]Channel, 0, len(reply.Status))
	for _, s := range reply.Status {
		out = append(out, Channel{Number: s.Channel, Name: s.Name, Online: s.Online != 0})
	}
	return out, nil
}

func nvrLogout(c *http.Client, base, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), nvrWait)
	defer cancel()
	body := []map[string]any{{"cmd": "Logout"}}
	_ = nvrCall(ctx, c, base+"?cmd=Logout&token="+url.QueryEscape(token), body, nil)
}

func nvrCall(ctx context.Context, c *http.Client, at string, body any, into any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, at, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("doorbell: the NVR answered %d", resp.StatusCode)
	}
	var replies []nvrReply
	if err := json.Unmarshal(raw, &replies); err != nil || len(replies) == 0 {
		return fmt.Errorf("doorbell: the NVR sent %q", raw)
	}
	r := replies[0]
	if r.Code != 0 {
		if r.Cmd == "Login" {
			return ErrNVRLogin
		}
		return fmt.Errorf("doorbell: the NVR refused %s: %s", r.Cmd, r.Error.Detail)
	}
	if into == nil {
		return nil
	}
	return json.Unmarshal(r.Value, into)
}
