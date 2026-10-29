package gatt

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	attErrorRsp          = 0x01
	attMTUReq            = 0x02
	attMTURsp            = 0x03
	attFindInfoReq       = 0x04
	attFindInfoRsp       = 0x05
	attFindByTypeReq     = 0x06
	attReadByTypeReq     = 0x08
	attReadByTypeRsp     = 0x09
	attReadReq           = 0x0a
	attReadRsp           = 0x0b
	attReadBlobReq       = 0x0c
	attReadBlobRsp       = 0x0d
	attReadByGroupReq    = 0x10
	attReadByGroupRsp    = 0x11
	attWriteReq          = 0x12
	attWriteRsp          = 0x13
	attPrepareWriteReq   = 0x16
	attPrepareWriteRsp   = 0x17
	attExecuteWriteReq   = 0x18
	attExecuteWriteRsp   = 0x19
	attNotification      = 0x1b
	attIndication        = 0x1d
	attConfirmation      = 0x1e
	attWriteCmd          = 0x52
	attCommandFlag       = 0x40
	attDefaultMTU        = 23
	attTimeout           = 30 * time.Second
	errInvalidHandle     = 0x01
	errRequestNotSupport = 0x06
	errInvalidOffset     = 0x07
	errAttributeNotFound = 0x0a
	errAttributeNotLong  = 0x0b
)

const CID = 0x0004

const MTU = 247

var ErrClosed = errors.New("gatt: the link is closed")

type Link interface {
	Send(ctx context.Context, cid uint16, payload []byte) error
	On(cid uint16, f func([]byte))
	Done() <-chan struct{}
	Disconnect() error
}

type Error struct {
	Request uint8
	Handle  uint16
	Code    uint8
}

func (e *Error) Error() string {
	return fmt.Sprintf(
		"gatt: request %#02x on handle %#04x failed: %#02x",
		e.Request,
		e.Handle,
		e.Code,
	)
}

type Client struct {
	l Link

	req    sync.Mutex
	mu     sync.Mutex
	mtu    int
	wait   chan []byte
	notify func(handle uint16, data []byte)
}

func New(l Link) *Client {
	c := &Client{l: l, mtu: attDefaultMTU}
	l.On(CID, c.receive)
	return c
}

func (c *Client) OnNotify(f func(handle uint16, data []byte)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notify = f
}

func (c *Client) MTU() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mtu
}

func (c *Client) Done() <-chan struct{} { return c.l.Done() }

func (c *Client) Disconnect() error { return c.l.Disconnect() }

func (c *Client) ExchangeMTU(ctx context.Context) (int, error) {
	req := []byte{attMTUReq, 0, 0}
	binary.LittleEndian.PutUint16(req[1:], MTU)
	rsp, err := c.request(ctx, req, attMTURsp)
	if err != nil {
		if e, ok := errors.AsType[*Error](err); ok && e.Code == errRequestNotSupport {
			return c.MTU(), nil
		}
		return 0, err
	}
	if len(rsp) < 2 {
		return 0, errors.New("gatt: short mtu response")
	}
	c.setMTU(int(binary.LittleEndian.Uint16(rsp)))
	return c.MTU(), nil
}

func (c *Client) Read(ctx context.Context, handle uint16) ([]byte, error) {
	req := []byte{attReadReq, 0, 0}
	binary.LittleEndian.PutUint16(req[1:], handle)
	value, err := c.request(ctx, req, attReadRsp)
	if err != nil {
		return nil, err
	}
	value = append([]byte(nil), value...)

	for len(value) > 0 && len(value)%(c.MTU()-1) == 0 {
		blob := []byte{attReadBlobReq, 0, 0, 0, 0}
		binary.LittleEndian.PutUint16(blob[1:], handle)
		binary.LittleEndian.PutUint16(blob[3:], uint16(len(value)))
		more, err := c.request(ctx, blob, attReadBlobRsp)
		if e, ok := errors.AsType[*Error](err); ok &&
			(e.Code == errAttributeNotLong || e.Code == errInvalidOffset) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(more) == 0 {
			break
		}
		value = append(value, more...)
		if len(more) < c.MTU()-1 {
			break
		}
	}
	return value, nil
}

func (c *Client) Write(ctx context.Context, handle uint16, data []byte, response bool) error {
	room := c.MTU() - 3
	if !response {
		if len(data) > room {
			return fmt.Errorf(
				"gatt: %d bytes do not fit a write without response (%d)",
				len(data),
				room,
			)
		}
		pdu := []byte{attWriteCmd, 0, 0}
		binary.LittleEndian.PutUint16(pdu[1:], handle)
		return c.send(ctx, append(pdu, data...))
	}

	if len(data) <= room {
		pdu := []byte{attWriteReq, 0, 0}
		binary.LittleEndian.PutUint16(pdu[1:], handle)
		_, err := c.request(ctx, append(pdu, data...), attWriteRsp)
		return err
	}

	part := c.MTU() - 5
	for offset := 0; offset < len(data); offset += part {
		end := min(offset+part, len(data))
		pdu := []byte{attPrepareWriteReq, 0, 0, 0, 0}
		binary.LittleEndian.PutUint16(pdu[1:], handle)
		binary.LittleEndian.PutUint16(pdu[3:], uint16(offset))
		if _, err := c.request(
			ctx,
			append(pdu, data[offset:end]...),
			attPrepareWriteRsp,
		); err != nil {
			_, _ = c.request(ctx, []byte{attExecuteWriteReq, 0x00}, attExecuteWriteRsp)
			return err
		}
	}
	_, err := c.request(ctx, []byte{attExecuteWriteReq, 0x01}, attExecuteWriteRsp)
	return err
}

func (c *Client) send(ctx context.Context, pdu []byte) error {
	if err := c.l.Send(ctx, CID, pdu); err != nil {
		select {
		case <-c.l.Done():
			return ErrClosed
		default:
			return err
		}
	}
	return nil
}

func (c *Client) request(ctx context.Context, pdu []byte, want uint8) ([]byte, error) {
	c.req.Lock()
	defer c.req.Unlock()

	wait := make(chan []byte, 1)
	c.mu.Lock()
	c.wait = wait
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.wait == wait {
			c.wait = nil
		}
		c.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(ctx, attTimeout)
	defer cancel()
	if err := c.send(ctx, pdu); err != nil {
		return nil, err
	}

	select {
	case rsp := <-wait:
		if rsp[0] == attErrorRsp {
			if len(rsp) < 5 {
				return nil, errors.New("gatt: short error response")
			}
			return nil, &Error{
				Request: rsp[1],
				Handle:  binary.LittleEndian.Uint16(rsp[2:]),
				Code:    rsp[4],
			}
		}
		if rsp[0] != want {
			return nil, fmt.Errorf("gatt: response %#02x to request %#02x", rsp[0], pdu[0])
		}
		return rsp[1:], nil
	case <-ctx.Done():
		// A transaction that times out leaves the bearer unusable until the link is re-established.
		go func() { _ = c.l.Disconnect() }()
		return nil, fmt.Errorf("gatt: request %#02x: %w", pdu[0], ctx.Err())
	case <-c.l.Done():
		return nil, ErrClosed
	}
}

func (c *Client) receive(p []byte) {
	if len(p) == 0 {
		return
	}
	op := p[0]
	switch {
	case op == attNotification || op == attIndication:
		if len(p) < 3 {
			return
		}
		if op == attIndication {
			c.reply([]byte{attConfirmation})
		}
		c.mu.Lock()
		notify := c.notify
		c.mu.Unlock()
		if notify != nil {
			notify(binary.LittleEndian.Uint16(p[1:]), append([]byte(nil), p[3:]...))
		}

	case op == attMTUReq:
		if len(p) >= 3 {
			c.setMTU(int(binary.LittleEndian.Uint16(p[1:])))
			rsp := []byte{attMTURsp, 0, 0}
			binary.LittleEndian.PutUint16(rsp[1:], MTU)
			c.reply(rsp)
		}

	case op == attErrorRsp || op&1 == 1 || op == attConfirmation:
		c.mu.Lock()
		if c.wait != nil {
			c.wait <- append([]byte(nil), p...)
			c.wait = nil
		}
		c.mu.Unlock()

	case op&attCommandFlag != 0:

	default:
		code := uint8(errRequestNotSupport)
		switch op {
		case attFindInfoReq, attReadByTypeReq, attReadByGroupReq, attFindByTypeReq:
			code = errAttributeNotFound
		case attReadReq, attReadBlobReq, attWriteReq, attPrepareWriteReq:
			code = errInvalidHandle
		}
		var handle uint16
		if len(p) >= 3 {
			handle = binary.LittleEndian.Uint16(p[1:])
		}
		rsp := []byte{attErrorRsp, op, 0, 0, code}
		binary.LittleEndian.PutUint16(rsp[2:], handle)
		c.reply(rsp)
	}
}

func (c *Client) reply(pdu []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), attTimeout)
	defer cancel()
	_ = c.send(ctx, pdu)
}

func (c *Client) setMTU(peer int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mtu = max(attDefaultMTU, min(MTU, peer))
}
