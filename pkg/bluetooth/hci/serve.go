package hci

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/bluetooth/pair"
)

type Classic interface {
	Event(pair.Event) ([]pair.Command, error)

	Data(handle uint16, pdu []byte) ([][]byte, error)

	Gone(handle uint16)
}

type Sends interface {
	Sending(send func(handle uint16, pdu []byte) error)
}

const awake = time.Second

const queued = 64

type outbound struct {
	raw []byte

	handle Handle
	pdu    []byte
}

func Serve(ctx context.Context, s *Session, on Classic) error {
	got, release := s.Take(s.classicWants)
	defer release()

	if _, _, err := s.pools(); err != nil {
		return err
	}

	ctx, stop := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { stop(); workers.Wait() }()

	out := make(chan outbound, queued)
	workers.Go(func() { writing(ctx, s, out) })

	if speaks, ok := on.(Sends); ok {
		speaks.Sending(func(handle uint16, pdu []byte) error {
			return post(ctx, out, outbound{handle: Handle(handle), pdu: pdu})
		})
		defer speaks.Sending(nil)
	}

	parts := newReassembler()

	for {
		var g packet
		select {
		case <-ctx.Done():
			return nil
		case have, ok := <-got:
			if !ok {
				return fmt.Errorf("ble: serving: %w", s.Err())
			}
			g = have
		}

		if g.kind == typeACL {
			if err := carry(ctx, on, parts, out, g.acl); err != nil {
				slog.Warn("bluetooth data", "err", err)
			}
			continue
		}

		if err := answer(ctx, on, parts, out, g.event); err != nil {
			slog.Warn("bluetooth event", "code", g.event.Code, "err", err)
		}
	}
}

func writing(ctx context.Context, s *Session, out <-chan outbound) {
	for {
		var o outbound

		select {
		case <-ctx.Done():
			return
		case o = <-out:
		}

		if o.raw != nil {
			if _, err := s.Write(o.raw); err != nil {
				slog.Warn("bluetooth writing a command", "err", err)
			}
			continue
		}

		if err := s.Send(ctx, o.handle, o.pdu); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("bluetooth writing data", "err", err)
		}
	}
}

func post(ctx context.Context, out chan<- outbound, o outbound) error {
	select {
	case out <- o:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("ble: %d things already waiting to go out", queued)
	}
}

func answer(
	ctx context.Context,
	on Classic,
	parts *reassembler,
	out chan<- outbound,
	e event,
) error {
	if e.Code == pair.EventDisconnectionComplete {
		if h, ok := pair.Handle(e.Params); ok {
			parts.forget(Handle(h))
			on.Gone(h)
		}
	}

	said, err := on.Event(pair.Event{Code: e.Code, Params: e.Params})
	if err != nil {
		return err
	}

	for _, c := range said {
		cmd, err := command(c.Opcode, c.Params...)
		if err != nil {
			return err
		}
		if err := post(ctx, out, outbound{raw: cmd}); err != nil {
			return fmt.Errorf("sending %s: %w", c, err)
		}
	}
	return nil
}

func carry(
	ctx context.Context,
	on Classic,
	parts *reassembler,
	out chan<- outbound,
	a aclPacket,
) error {
	pdu, err := parts.add(a)
	if err != nil {
		return err
	}
	if pdu == nil {
		return nil
	}

	said, err := on.Data(uint16(a.Handle), pdu)
	if err != nil {
		return err
	}

	for _, reply := range said {
		if err := post(ctx, out, outbound{handle: a.Handle, pdu: reply}); err != nil {
			return err
		}
	}
	return nil
}

func buffers(s *Session) (size, total int, err error) {
	said, err := s.Command(hciReadBufferSize)
	if err != nil {
		return 0, 0, err
	}
	return parseBufferSize(said)
}
