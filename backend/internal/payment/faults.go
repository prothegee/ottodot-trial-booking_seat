package payment

// Fault is how a deliberately injected failure reaches this package.
//
// It is a function rather than the fault registry itself, so this package
// depends on nothing to be broken on purpose. Nil is the ordinary state and
// means nothing is ever injected.
type Fault func(point string) bool

// triggered reports whether the named point should fail this time.
func (fault Fault) triggered(point string) bool {
    return fault != nil && fault(point)
}
