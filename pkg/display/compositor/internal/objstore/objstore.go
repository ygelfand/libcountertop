package objstore

import (
	"sync"

	"github.com/ygelfand/libcountertop/pkg/display/compositor/internal/debug"
	"github.com/ygelfand/libcountertop/pkg/display/compositor/internal/wire"
)

type Store struct {
	mu      sync.Mutex
	objects map[uint32]wire.Object
	nextID  uint32
}

func New(start uint32) *Store {
	return &Store{
		objects: make(map[uint32]wire.Object),
		nextID:  start,
	}
}

func (s *Store) Add(obj wire.Object) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := obj.ID()
	if id == 0 {
		id = s.nextID
		obj.SetID(id)
		s.nextID++
	}

	s.objects[id] = obj
}

func (s *Store) Get(id uint32) wire.Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.objects[id]
}

func (s *Store) Delete(id uint32) {
	s.mu.Lock()
	obj := s.objects[id]
	delete(s.objects, id)
	s.mu.Unlock()
	if obj != nil {
		obj.Delete()
	}
}

func (s *Store) Clear() {
	s.mu.Lock()
	objects := s.objects
	s.objects = make(map[uint32]wire.Object)
	s.mu.Unlock()
	for _, obj := range objects {
		obj.Delete()
	}
}

func (s *Store) Dispatch(msg *wire.MessageBuffer) error {
	obj := s.Get(msg.Sender())
	if obj == nil {
		return wire.UnknownSenderIDError{Msg: msg}
	}

	err := obj.Dispatch(msg)
	debug.Printf("%v", msg.Debug(obj))
	return err
}
