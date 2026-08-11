package booking

// Fault is how a deliberately injected failure reaches this package.
//
// It is a function rather than the fault registry itself, so this package
// depends on nothing to be broken on purpose and a test can break it with a
// closure. Nil is the ordinary state and means nothing is ever injected, which
// is what every caller outside a demonstration hands in.
//
// The point names it is called with come from `internal/faults`, which is the
// one place they are written down. A call site here names the constant rather
// than the string, so a rename is a compile failure instead of a fault that
// silently stops firing.
type Fault func(point string) bool

// triggered reports whether the named point should fail this time.
//
// The nil check lives here rather than at each call site. Two call sites in one
// file each needing to remember it is exactly how the second one ends up without
// it.
func (fault Fault) triggered(point string) bool {
    return fault != nil && fault(point)
}
