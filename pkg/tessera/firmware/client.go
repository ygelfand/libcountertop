package firmware

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"github.com/ygelfand/go-esphome-device/api"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const frameMost = 1 << 20

var messages = sync.OnceValue(func() map[uint32]protoreflect.MessageType {
	out := map[uint32]protoreflect.MessageType{}
	protoregistry.GlobalTypes.RangeMessages(func(mt protoreflect.MessageType) bool {
		opts := mt.Descriptor().Options()
		if opts != nil && proto.HasExtension(opts, api.E_Id) {
			out[proto.GetExtension(opts, api.E_Id).(uint32)] = mt
		}
		return true
	})
	return out
})

const helloMost = 16

type Client struct {
	conn  net.Conn
	in    *bufio.Reader
	early []proto.Message

	mu sync.Mutex
}

func Dial(ctx context.Context, client string) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(APIPort)))
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, in: bufio.NewReader(conn)}
	if err := c.Send(
		&api.HelloRequest{ClientInfo: client, ApiVersionMajor: 1, ApiVersionMinor: 10},
	); err != nil {
		return nil, errors.Join(err, conn.Close())
	}
	for range helloMost {
		m, err := c.read()
		if err != nil {
			return nil, errors.Join(err, conn.Close())
		}
		if _, ok := m.(*api.HelloResponse); ok {
			return c, nil
		}
		c.early = append(c.early, m)
	}
	return nil, errors.Join(errors.New("tessera: no hello answer"), conn.Close())
}

func (c *Client) Send(m proto.Message) error {
	opts := m.ProtoReflect().Descriptor().Options()
	if !proto.HasExtension(opts, api.E_Id) {
		return fmt.Errorf("tessera: %T has no message id", m)
	}
	body, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	frame := []byte{0}
	frame = binary.AppendUvarint(frame, uint64(len(body)))
	frame = binary.AppendUvarint(frame, uint64(proto.GetExtension(opts, api.E_Id).(uint32)))
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.conn.Write(append(frame, body...))
	return err
}

func (c *Client) Recv() (proto.Message, error) {
	if len(c.early) > 0 {
		m := c.early[0]
		c.early = c.early[1:]
		return m, nil
	}
	return c.read()
}

func (c *Client) read() (proto.Message, error) {
	for {
		if b, err := c.in.ReadByte(); err != nil {
			return nil, err
		} else if b != 0 {
			return nil, fmt.Errorf("tessera: frame starts with %#x", b)
		}
		size, err := binary.ReadUvarint(c.in)
		if err != nil {
			return nil, err
		}
		kind, err := binary.ReadUvarint(c.in)
		if err != nil {
			return nil, err
		}
		if size > frameMost {
			return nil, fmt.Errorf("tessera: %d byte frame", size)
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(c.in, body); err != nil {
			return nil, err
		}
		mt, ok := messages()[uint32(kind)]
		if !ok {
			continue
		}
		m := mt.New().Interface()
		if err := proto.Unmarshal(body, m); err != nil {
			return nil, fmt.Errorf("tessera: message %d: %w", kind, err)
		}
		return m, nil
	}
}

func (c *Client) Close() error { return c.conn.Close() }
