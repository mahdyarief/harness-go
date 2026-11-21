package scope

type Event[T any] struct {
	Name    string
	Payload T
}

type listener[T any] func(*Event[T]) error

type listenerBox[T any] struct{ fns []listener[T] }

func eventBoxKey[T any](name string) Key[*listenerBox[T]] {
	return NewKey[*listenerBox[T]]("event:" + name)
}

func On[T any](s *Scope, name string, fn listener[T]) Disposer {
	key := eventBoxKey[T](name)
	box, ok := Require(s, key)
	if !ok {
		box = &listenerBox[T]{}
		_ = s.Provide(key, box, nil)
	}
	box.fns = append(box.fns, fn)
	return func() error { return nil }
}

func listeners[T any](s *Scope, name string) []listener[T] {
	box, ok := Require(s, eventBoxKey[T](name))
	if !ok {
		return nil
	}
	return box.fns
}

func Emit[T any](s *Scope, ev Event[T]) error {
	for _, fn := range listeners[T](s, ev.Name) {
		if err := fn(&ev); err != nil {
			return err
		}
	}
	return nil
}

func Waterfall[T any](s *Scope, ev Event[T], seed T) (T, error) {
	ev.Payload = seed
	for _, fn := range listeners[T](s, ev.Name) {
		if err := fn(&ev); err != nil {
			return ev.Payload, err
		}
	}
	return ev.Payload, nil
}

func Bail[T any](s *Scope, ev Event[T]) (bool, error) {
	for _, fn := range listeners[T](s, ev.Name) {
		if err := fn(&ev); err != nil {
			return true, err
		}
	}
	return false, nil
}
