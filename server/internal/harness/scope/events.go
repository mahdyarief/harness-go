package scope

import (
	"errors"
	"sync"
)

var (
	ErrBail     = errors.New("event bailed")
	errNoListen = errors.New("listener not registered")
	errDisposed = errors.New("listener already disposed")
)

type Event[T any] struct {
	Name    string
	Payload T
}

type listener[T any] struct {
	id uint64
	fn func(*Event[T]) error
}

type listenerBox[T any] struct {
	mu     sync.Mutex
	nextID uint64
	fns    []listener[T]
	next   []nextListener
}

type nextListener struct {
	id uint64
	fn func(func() error) func() error
}

type EventOptions struct {
	Prepend bool
	Global  bool
}

func eventBoxKey[T any](name string) Key[*listenerBox[T]] {
	return NewKey[*listenerBox[T]]("event:" + name)
}

func boxFor[T any](s *Scope, name string) *listenerBox[T] {
	key := eventBoxKey[T](name)
	box, ok := Require(s, key)
	if !ok {
		box = &listenerBox[T]{}
		_ = s.Provide(key, box, nil)
	}
	return box
}

func removeByID[T any](box *listenerBox[T], id uint64) error {
	box.mu.Lock()
	defer box.mu.Unlock()
	for i, existing := range box.fns {
		if existing.id == id {
			box.fns = append(box.fns[:i:i], box.fns[i+1:]...)
			return nil
		}
	}
	return errNoListen
}

func On[T any](s *Scope, name string, fn func(*Event[T]) error, options ...EventOptions) Disposer {
	box := boxFor[T](s, name)
	box.mu.Lock()
	box.nextID++
	id := box.nextID
	entry := listener[T]{id: id, fn: fn}
	if len(options) > 0 && options[0].Prepend {
		box.fns = append([]listener[T]{entry}, box.fns...)
	} else {
		box.fns = append(box.fns, entry)
	}
	box.mu.Unlock()

	removed := false
	return func() error {
		if removed {
			return errDisposed
		}
		if err := removeByID(box, id); err != nil {
			return err
		}
		removed = true
		return nil
	}
}

func Once[T any](s *Scope, name string, fn func(*Event[T]) error, options ...EventOptions) Disposer {
	var dispose Disposer
	dispose = On(s, name, func(ev *Event[T]) error {
		_ = dispose()
		return fn(ev)
	}, options...)
	return dispose
}

func listeners[T any](s *Scope, name string) []listener[T] {
	box, ok := Require(s, eventBoxKey[T](name))
	if !ok {
		return nil
	}
	box.mu.Lock()
	defer box.mu.Unlock()
	return append([]listener[T](nil), box.fns...)
}

func Emit[T any](s *Scope, ev Event[T]) error {
	for _, entry := range listeners[T](s, ev.Name) {
		if err := entry.fn(&ev); err != nil {
			return err
		}
	}
	return nil
}

func Parallel[T any](s *Scope, ev Event[T]) error {
	fns := listeners[T](s, ev.Name)
	errCh := make(chan error, len(fns))
	var wg sync.WaitGroup
	for _, entry := range fns {
		wg.Add(1)
		go func(fn func(*Event[T]) error) {
			defer wg.Done()
			if err := fn(&ev); err != nil {
				errCh <- err
			}
		}(entry.fn)
	}
	wg.Wait()
	close(errCh)
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func Serial[T any](s *Scope, ev Event[T]) error {
	for _, entry := range listeners[T](s, ev.Name) {
		if err := entry.fn(&ev); err != nil {
			return err
		}
	}
	return nil
}

func Waterfall[T any](s *Scope, ev Event[T], seed T) (T, error) {
	ev.Payload = seed
	for _, entry := range listeners[T](s, ev.Name) {
		if err := entry.fn(&ev); err != nil {
			return ev.Payload, err
		}
	}
	return ev.Payload, nil
}

func Bail[T any](s *Scope, ev Event[T]) (bool, error) {
	for _, entry := range listeners[T](s, ev.Name) {
		if err := entry.fn(&ev); err != nil {
			return true, err
		}
	}
	return false, nil
}

func OnNext(s *Scope, name string, fn func(func() error) func() error, options ...EventOptions) Disposer {
	box := boxFor[any](s, name)
	box.mu.Lock()
	box.nextID++
	id := box.nextID
	entry := nextListener{id: id, fn: fn}
	if len(options) > 0 && options[0].Prepend {
		box.next = append([]nextListener{entry}, box.next...)
	} else {
		box.next = append(box.next, entry)
	}
	box.mu.Unlock()

	removed := false
	return func() error {
		if removed {
			return errDisposed
		}
		box.mu.Lock()
		defer box.mu.Unlock()
		for i, existing := range box.next {
			if existing.id == id {
				box.next = append(box.next[:i:i], box.next[i+1:]...)
				removed = true
				return nil
			}
		}
		return errNoListen
	}
}

func WaterfallNext(s *Scope, name string, base func() error) error {
	box, ok := Require(s, eventBoxKey[any](name))
	if !ok {
		if base == nil {
			return nil
		}
		return base()
	}
	box.mu.Lock()
	chain := append([]nextListener(nil), box.next...)
	box.mu.Unlock()

	next := func() error {
		if base == nil {
			return nil
		}
		return base()
	}
	for i := len(chain) - 1; i >= 0; i-- {
		fn := chain[i].fn
		current := next
		next = func() error {
			return fn(current)()
		}
	}
	return next()
}

