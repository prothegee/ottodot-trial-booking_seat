package observability

import (
    "context"
    "log/slog"
)

// redactingHandler puts every line through the redaction rules on its way out.
//
// It wraps another handler rather than being one, which is what makes the
// promise in the sensitive data rules true: redaction happens at the writer, so
// there is no call site that could forget it and no code path that could reach
// the output without passing through here.
//
// Both halves of a line are scrubbed. The message, because a wrapped driver
// error can carry a whole request inside its text, and the attributes, because a
// field named `cookie` must lose its value whatever the message says.
type redactingHandler struct {
    inner slog.Handler
}

// Enabled passes the level question straight through.
func (handler redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
    return handler.inner.Enabled(ctx, level)
}

// Handle scrubs the record and hands it on.
func (handler redactingHandler) Handle(ctx context.Context, record slog.Record) error {
    scrubbed := slog.NewRecord(record.Time, record.Level, RedactText(record.Message), record.PC)

    record.Attrs(func(attribute slog.Attr) bool {
        scrubbed.AddAttrs(redactAttribute(attribute))

        return true
    })

    return handler.inner.Handle(ctx, scrubbed)
}

// WithAttrs scrubs the bound attributes too.
//
// A logger built with `With("cookie", value)` would otherwise carry that value on
// every later line without any of them passing through Handle's attribute walk.
func (handler redactingHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
    scrubbed := make([]slog.Attr, 0, len(attributes))

    for _, attribute := range attributes {
        scrubbed = append(scrubbed, redactAttribute(attribute))
    }

    return redactingHandler{inner: handler.inner.WithAttrs(scrubbed)}
}

// WithGroup passes the group through, keeping this handler in the chain.
func (handler redactingHandler) WithGroup(name string) slog.Handler {
    return redactingHandler{inner: handler.inner.WithGroup(name)}
}

// redactAttribute applies the rules to one field.
//
// A group is walked rather than trusted. Nesting is the obvious way for a
// sensitive value to arrive without the top level field name giving it away.
func redactAttribute(attribute slog.Attr) slog.Attr {
    value := attribute.Value.Resolve()

    if value.Kind() == slog.KindGroup {
        members := value.Group()
        scrubbed := make([]slog.Attr, 0, len(members))

        for _, member := range members {
            scrubbed = append(scrubbed, redactAttribute(member))
        }

        return slog.Attr{Key: attribute.Key, Value: slog.GroupValue(scrubbed...)}
    }

    if SensitiveField(attribute.Key) {
        return slog.String(attribute.Key, Redacted)
    }

    if value.Kind() == slog.KindString {
        return slog.String(attribute.Key, RedactText(value.String()))
    }

    // A number, a boolean, or a duration carries nothing that can be scrubbed
    // and nothing that could hide an address, so it is passed on untouched.
    return slog.Attr{Key: attribute.Key, Value: value}
}
