package discovery

import (
	"context"
	"log/slog"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/zeroconf/v2"

	"github.com/ygelfand/libcountertop/pkg/hook"
	mdns "github.com/ygelfand/libcountertop/pkg/network/advertise"
	sharedpeer "github.com/ygelfand/libcountertop/pkg/network/peer"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

const (
	service = "_countertop._tcp"
	domain  = "local."
	proto   = "1"
	ttl     = 120
	listen  = 4 * time.Second
	every   = 30 * time.Second
	missed  = 6
)

type Peer = sharedpeer.Peer
type Identity interface{ Self() Peer }
type Judge interface {
	Standing(id string) sharedpeer.Standing
}

type Discovery struct {
	identity Identity
	judge    Judge
	Changed  hook.Hook[struct{}]
	Found    hook.Hook[Peer]
	Lost     hook.Hook[Peer]
	mu       sync.Mutex
	peers    map[string]Peer
}

func New(identity Identity, judge Judge) *Discovery {
	return &Discovery{identity: identity, judge: judge, peers: map[string]Peer{}}
}
func (d *Discovery) Name() string { return "discovery" }

func (d *Discovery) Self() Peer { return d.identity.Self() }

func (d *Discovery) Peers() []Peer {
	d.mu.Lock()
	out := make([]Peer, 0, len(d.peers))
	for _, p := range d.peers {
		out = append(out, clonePeer(p))
	}
	d.mu.Unlock()
	for i := range out {
		out[i].Standing = d.judge.Standing(out[i].ID)
	}
	slices.SortFunc(
		out,
		func(a, b Peer) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) },
	)
	return out
}

func (d *Discovery) PeerNames() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, 0, len(d.peers))
	for _, p := range d.peers {
		out = append(out, p.Name)
	}
	return out
}

func (d *Discovery) Touch(id string) {
	d.mu.Lock()
	if p, ok := d.peers[id]; ok {
		p.Seen = time.Now()
		d.peers[id] = p
	}
	d.mu.Unlock()
}

func (d *Discovery) Find(id string) (Peer, bool) {
	d.mu.Lock()
	p, ok := d.peers[id]
	d.mu.Unlock()
	p = clonePeer(p)
	if ok {
		p.Standing = d.judge.Standing(id)
	}
	return p, ok
}

func (d *Discovery) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	self := d.Self().ID
	advertised := make(chan struct{})
	safe.Go("discovery advertise", func() { defer close(advertised); d.advertise(ctx) })
	defer func() { cancel(); <-advertised }()
	for {
		d.sweep(ctx, self)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(every):
		}
	}
}

func record(p Peer) []string {
	return []string{
		"id=" + p.ID,
		"name=" + p.Name,
		"model=" + p.Model,
		"board=" + p.Board,
		"project=" + p.Project,
		"version=" + p.Version,
		"proto=" + proto,
		"caps=" + strings.Join(p.Caps, ","),
		"secure=" + strconv.Itoa(p.Secure),
	}
}
func (d *Discovery) advertise(ctx context.Context) {
	mdns.Advertise(ctx, "countertop", func(ips []net.IP) (func(), error) {
		p := d.Self()
		srv, err := zeroconf.RegisterProxy(
			p.Name,
			service,
			domain,
			p.Port,
			p.Host,
			mdns.Strings(ips),
			record(p),
			nil,
			zeroconf.TTL(ttl),
		)
		if err != nil {
			return nil, err
		}
		return srv.Shutdown, nil
	})
}
func (d *Discovery) sweep(ctx context.Context, self string) {
	ctx, cancel := context.WithTimeout(ctx, listen)
	defer cancel()
	entries := make(chan *zeroconf.ServiceEntry, 8)
	done := make(chan struct{})
	safe.Go("discovery browse", func() {
		defer close(done)
		if err := zeroconf.Browse(ctx, service, domain, entries); err != nil {
			slog.Debug("countertop browse", "err", err)
		}
	})
	now := time.Now()
	for {
		var e *zeroconf.ServiceEntry
		select {
		case <-ctx.Done():
			<-done
			d.expire(now)
			return
		case <-done:
			d.expire(now)
			return
		case next, ok := <-entries:
			if !ok {
				<-done
				d.expire(now)
				return
			}
			e = next
		}

		p := parse(e, now)
		if p.ID == "" || p.ID == self {
			continue
		}
		d.mu.Lock()
		_, known := d.peers[p.ID]
		d.peers[p.ID] = p
		d.mu.Unlock()
		if !known {
			slog.Info("peer found", "name", p.Name, "model", p.Model, "addrs", p.Addrs)
			d.Found.Emit(clonePeer(p))
			d.Changed.Emit(struct{}{})
		}
	}
}

func (d *Discovery) expire(now time.Time) {
	gone := now.Add(-missed * every)
	d.mu.Lock()
	var lost []Peer
	for k, p := range d.peers {
		if p.Seen.Before(gone) {
			lost = append(lost, p)
			delete(d.peers, k)
		}
	}
	d.mu.Unlock()
	for _, p := range lost {
		slog.Info("peer lost", "name", p.Name)
		d.Lost.Emit(p)
	}
	if len(lost) > 0 {
		d.Changed.Emit(struct{}{})
	}
}

func parse(e *zeroconf.ServiceEntry, now time.Time) Peer {
	txt := map[string]string{}
	for _, kv := range e.Text {
		k, v, _ := strings.Cut(kv, "=")
		txt[strings.ToLower(k)] = v
	}
	p := Peer{
		ID:      txt["id"],
		Name:    txt["name"],
		Model:   txt["model"],
		Board:   txt["board"],
		Project: txt["project"],
		Version: txt["version"],
		Host:    strings.TrimSuffix(e.HostName, "."),
		Addrs:   append(append([]net.IP(nil), e.AddrIPv4...), e.AddrIPv6...),
		Port:    e.Port,
		Seen:    now,
	}
	if c := txt["caps"]; c != "" {
		p.Caps = strings.Split(c, ",")
	}
	if n, err := strconv.Atoi(txt["secure"]); err == nil {
		p.Secure = n
	}
	if p.Name == "" {
		p.Name = e.Instance
	}
	return p
}

func clonePeer(p Peer) Peer {
	p.Caps = slices.Clone(p.Caps)
	ips := make([]net.IP, len(p.Addrs))
	for i, ip := range p.Addrs {
		ips[i] = slices.Clone(ip)
	}
	p.Addrs = ips
	return p
}
