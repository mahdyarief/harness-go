package team

// Rule is one named branch in an ordered reducer.
type Rule[T any] struct {
	Name   string
	Match  func([]MemberResult) bool
	Output func([]MemberResult) T
}

// Rules evaluates in order and returns the first matching output and rule name.
type Rules[T any] []Rule[T]

func (rules Rules[T]) Reduce(results []MemberResult) (T, string, bool) {
	for _, rule := range rules {
		if rule.Match(results) {
			return rule.Output(results), rule.Name, true
		}
	}
	var zero T
	return zero, "", false
}
