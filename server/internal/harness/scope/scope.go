package scope

import (
	"errors"
	"fmt"
)

type Key[T any] struct{ name string }

func NewKey[T any](name string) Key[T] { return Key[T]{name: name} }
func (k Key[T]) String() string        { return k.name }

type Disposer func() error

type service struct {
	value   any
	dispose Disposer
}

type Scope struct {
	parent   *Scope
	services map[string]service
	order    []string
}

func New(parent *Scope) *Scope {
	return &Scope{parent: parent, services: map[string]service{}}
}

func (s *Scope) Provide(key fmt.Stringer, value any, dispose Disposer) error {
	name := key.String()
	if _, exists := s.services[name]; exists {
		return fmt.Errorf("service %q already provided in scope", name)
	}
	s.services[name] = service{value: value, dispose: dispose}
	s.order = append(s.order, name)
	return nil
}

func Require[T any](s *Scope, key Key[T]) (T, bool) {
	var zero T
	for cur := s; cur != nil; cur = cur.parent {
		svc, ok := cur.services[key.String()]
		if !ok {
			continue
		}
		v, ok := svc.value.(T)
		return v, ok
	}
	return zero, false
}

func (s *Scope) Child() *Scope { return New(s) }

func (s *Scope) Dispose() error {
	var errs []error
	for i := len(s.order) - 1; i >= 0; i-- {
		svc := s.services[s.order[i]]
		if svc.dispose != nil {
			if err := svc.dispose(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	s.services = map[string]service{}
	s.order = nil
	return errors.Join(errs...)
}
