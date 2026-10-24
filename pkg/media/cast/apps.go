package cast

import (
	"context"
	"errors"
	"sync"
)

type Protocol interface {
	Name() string
	Namespaces() []string

	Receive(app *Application, m Message) ([]Message, error)

	Run(ctx context.Context)
}

type Owner interface {
	Protocol
	Owns(a App) bool
}

type Loader interface {
	Load(app *Application, m Message) ([]Message, error)
}

type Starter interface {
	Started(app App)
	Ended(app App)
}

func (r *Receiver) bracket(a App, started bool) {
	for _, p := range r.speaks(a) {
		s, ok := p.(Starter)
		switch {
		case !ok:
		case started:
			s.Started(a)
		default:
			s.Ended(a)
		}
	}
}

var ErrUnspoken = errors.New("cast: not a message this protocol handles")

type App struct {
	ID   string
	Name string

	Icon string

	Protocols []string
}

var Apps = []App{
	{ID: "233637DE", Protocols: []string{"youtube"}},
	{ID: "2DB7CC49", Protocols: []string{"youtube"}},
}

var byID = sync.OnceValue(func() map[string]App {
	out := make(map[string]App, len(Apps))
	for _, a := range Apps {
		out[a.ID] = a
	}
	return out
})

func Lookup(id string) App {
	a, ok := byID()[id]
	if !ok {
		a = App{ID: id}
	}
	return a
}

func (r *Receiver) Register(p Protocol) {
	if r.protocols == nil {
		r.protocols = map[string]Protocol{}
	}
	r.protocols[p.Name()] = p
}

func (r *Receiver) speaks(a App) []Protocol {
	if o := r.ownerOf(a); o != nil {
		return []Protocol{o}
	}
	var out []Protocol
	for _, name := range a.Protocols {
		if p, ok := r.protocols[name]; ok {
			out = append(out, p)
		}
	}
	return out
}

func (r *Receiver) ownerOf(a App) Owner {
	for _, name := range a.Protocols {
		if _, ok := r.protocols[name]; ok {
			return nil
		}
	}
	for _, p := range r.protocols {
		if o, ok := p.(Owner); ok && o.Owns(a) {
			return o
		}
	}
	return nil
}

func (r *Receiver) owner() Owner {
	if r.app == nil {
		return nil
	}
	return r.ownerOf(Lookup(r.app.AppID))
}

func (r *Receiver) Advertise(app, name string, names []string) []Message {
	if r.app == nil || r.app.AppID != app {
		return nil
	}
	if name != "" {
		r.app.DisplayName, r.app.StatusText = name, name
	}
	r.app.Namespaces = r.app.Namespaces[:0]
	for _, n := range names {
		r.app.Namespaces = append(r.app.Namespaces, Namespace{Name: n})
	}
	return []Message{r.broadcast()}
}

func (r *Receiver) namespaces(a App) []Namespace {
	out := []Namespace{{Name: NSMedia}}
	seen := map[string]bool{NSMedia: true}
	for _, p := range r.speaks(a) {
		for _, n := range p.Namespaces() {
			if !seen[n] {
				seen[n] = true
				out = append(out, Namespace{Name: n})
			}
		}
	}
	return out
}

func (r *Receiver) loader() Loader {
	if r.app == nil {
		return nil
	}
	for _, p := range r.speaks(Lookup(r.app.AppID)) {
		if l, ok := p.(Loader); ok {
			return l
		}
	}
	return nil
}

func (r *Receiver) protocol(namespace string) Protocol {
	if r.app == nil {
		return nil
	}
	for _, p := range r.speaks(Lookup(r.app.AppID)) {
		for _, n := range p.Namespaces() {
			if n == namespace {
				return p
			}
		}
	}
	return nil
}
