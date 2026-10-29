package trust

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/network/peer"
)

const (
	firstRetry  = 5 * time.Second
	lastRetry   = 5 * time.Minute
	askWait     = 5 * time.Second
	pendingLife = 30 * time.Second
	window      = 5 * time.Minute
	burst       = 20
	ceiling     = 200
)

var (
	ErrNoCertificate = errors.New("trust: the peer sent no certificate")
	ErrForeign       = errors.New("trust: the peer is not in this home")
)

type Options struct {
	ID       string
	Identity tls.Certificate
	Key      func() string
	Peers    func() []peer.Peer
	Changed  func()
	Now      func() time.Time
}

type pending struct {
	secret []byte
	from   string
	at     time.Time
}

type Trust struct {
	o       Options
	self    string
	home    http.RoundTripper
	any     http.RoundTripper
	ctx     context.Context
	stop    context.CancelFunc
	workers sync.WaitGroup

	mu      sync.Mutex
	key     string
	byID    map[string]peer.Standing
	byCert  map[string]peer.Standing
	pending map[string]pending
	chasing map[string]context.CancelFunc
	asked   map[string][]time.Time
}

func New(o Options) (*Trust, error) {
	if len(o.Identity.Certificate) == 0 {
		return nil, ErrNoCertificate
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	ctx, stop := context.WithCancel(context.Background())
	t := &Trust{
		o:       o,
		self:    Fingerprint(o.Identity.Certificate[0]),
		ctx:     ctx,
		stop:    stop,
		chasing: map[string]context.CancelFunc{},
		asked:   map[string][]time.Time{},
	}
	t.forget(o.Key())
	t.home = t.transport(func(fp string) error {
		if !t.homeCert(fp) {
			return ErrForeign
		}
		return nil
	})
	t.any = t.transport(func(string) error { return nil })
	return t, nil
}

func (t *Trust) Name() string { return "trust" }

func (t *Trust) Run(ctx context.Context) error {
	for _, p := range t.o.Peers() {
		t.Found(p)
	}
	select {
	case <-ctx.Done():
	case <-t.ctx.Done():
	}
	t.stop()
	t.workers.Wait()
	return nil
}

func (t *Trust) known(id string) (peer.Peer, bool) {
	if id == "" || t.o.Peers == nil {
		return peer.Peer{}, false
	}
	for _, p := range t.o.Peers() {
		if p.ID == id {
			return p, true
		}
	}
	return peer.Peer{}, false
}

func (t *Trust) Found(p peer.Peer) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ctx.Err() != nil {
		return
	}
	if cancel := t.chasing[p.ID]; cancel != nil {
		cancel()
	}
	ctx, cancel := context.WithCancel(t.ctx)
	t.chasing[p.ID] = cancel
	t.workers.Go(func() { t.chase(ctx, p) })
}

func (t *Trust) Lost(p peer.Peer) {
	t.mu.Lock()
	if cancel := t.chasing[p.ID]; cancel != nil {
		cancel()
		delete(t.chasing, p.ID)
	}
	delete(t.byID, p.ID)
	t.mu.Unlock()
}

func (t *Trust) Rekey() {
	key := t.o.Key()
	t.mu.Lock()
	forgot := t.rekey(key)
	t.mu.Unlock()
	t.reconfirm(forgot)
}

func (t *Trust) reconfirm(forgot bool) {
	if !forgot {
		return
	}
	for _, p := range t.o.Peers() {
		t.Found(p)
	}
	t.changed()
}

func (t *Trust) chase(ctx context.Context, p peer.Peer) {
	wait := firstRetry
	for {
		key := t.o.Key()
		before := t.Standing(p.ID)
		if t.confirm(ctx, p, key) {
			if t.Standing(p.ID) != before {
				t.changed()
			}
			return
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		wait = min(2*wait, lastRetry)
	}
}

func (t *Trust) changed() {
	if t.o.Changed != nil {
		t.o.Changed()
	}
}

func (t *Trust) Standing(id string) peer.Standing {
	key := t.o.Key()
	t.mu.Lock()
	forgot := t.rekey(key)
	s, ok := t.byID[id]
	t.mu.Unlock()
	t.reconfirm(forgot)
	if !ok {
		return peer.Checking
	}
	return s
}

func (t *Trust) Identity() *tls.Certificate { return &t.o.Identity }

func (t *Trust) ServerConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{t.o.Identity},
		ClientAuth:   tls.RequestClientCert,
		MinVersion:   tls.VersionTLS13,
	}
}

func (t *Trust) Client(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: t.home}
}

func (t *Trust) AnyClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: t.any}
}

func (t *Trust) Home(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fp, ok := caller(r)
		if !ok || !t.homeCert(fp) {
			http.Error(w, "not in this home", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// TODO: remove Legacy and the plain-HTTP peer routes once no device predates the secure port.
func (t *Trust) Legacy(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if t.o.Key() != "" {
			http.Error(w, "not in this home", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (t *Trust) homeCert(fp string) bool {
	key := t.o.Key()
	t.mu.Lock()
	forgot := t.rekey(key)
	home := t.byCert[fp] == peer.Home
	t.mu.Unlock()
	t.reconfirm(forgot)
	return home
}

func (t *Trust) transport(check func(fp string) error) *http.Transport {
	cfg := &tls.Config{
		Certificates:       []tls.Certificate{t.o.Identity},
		InsecureSkipVerify: true, //nolint:gosec // peers are self-signed; VerifyConnection checks them
		MinVersion:         tls.VersionTLS13,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return ErrNoCertificate
			}
			return check(Fingerprint(cs.PeerCertificates[0].Raw))
		},
	}
	return &http.Transport{TLSClientConfig: cfg, ForceAttemptHTTP2: true}
}

func (t *Trust) rekey(key string) bool {
	if key == t.key {
		return false
	}
	t.forget(key)
	return true
}

func (t *Trust) forget(key string) {
	t.key = key
	t.byID = map[string]peer.Standing{}
	t.byCert = map[string]peer.Standing{}
	t.pending = map[string]pending{}
}

func (t *Trust) judge(key, id, fp string, v peer.Standing) {
	if key != t.o.Key() {
		return
	}
	t.mu.Lock()
	forgot := t.rekey(key)
	if id != "" {
		t.byID[id] = v
	}
	if fp != "" {
		t.byCert[fp] = v
	}
	t.mu.Unlock()
	t.reconfirm(forgot)
}

func caller(r *http.Request) (string, bool) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return "", false
	}
	return Fingerprint(r.TLS.PeerCertificates[0].Raw), true
}

func certOf(cs tls.ConnectionState) (*x509.Certificate, bool) {
	if len(cs.PeerCertificates) == 0 {
		return nil, false
	}
	return cs.PeerCertificates[0], true
}
