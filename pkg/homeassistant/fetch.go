package homeassistant

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

const (
	FetchProtocol = 4

	fetchAction = "echolocal.fetch"
)

var ErrNoFetch = errors.New("homeassistant: no connected Home Assistant downloads for this device")

type Piece struct {
	Status int
	Total  int64
	Type   string
	Data   []byte
}

func (h *Controller) Fetch(ctx context.Context, url string, offset, length int64) (Piece, error) {
	conn, ok := h.speaker(FetchProtocol)
	if !ok {
		return Piece{}, ErrNoFetch
	}
	data := map[string]string{
		"url":    url,
		"offset": strconv.FormatInt(offset, 10),
		"length": strconv.FormatInt(length, 10),
	}
	raw, err := h.send(ctx, []sender{conn}, request(fetchAction, data, nil, pingReply))
	if err != nil {
		return Piece{}, err
	}
	var out struct {
		Response json.RawMessage `json:"response"`
	}
	var answer struct {
		Status int    `json:"status"`
		Total  int64  `json:"total"`
		Type   string `json:"type"`
		Data   string `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Piece{}, fmt.Errorf("homeassistant: the fetch response: %w", err)
	}
	if err := json.Unmarshal(unwrap(out.Response), &answer); err != nil {
		return Piece{}, fmt.Errorf("homeassistant: the fetch answer: %w", err)
	}
	body, err := base64.StdEncoding.DecodeString(answer.Data)
	if err != nil {
		return Piece{}, fmt.Errorf("homeassistant: the fetched bytes: %w", err)
	}
	return Piece{Status: answer.Status, Total: answer.Total, Type: answer.Type, Data: body}, nil
}
