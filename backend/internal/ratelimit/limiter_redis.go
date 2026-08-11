package ratelimit

import (
    "context"
    "time"

    "github.com/redis/go-redis/v9"
)

// takeScript is the whole decision, run inside Redis.
//
// It is a script rather than a read followed by a write for one reason: two
// requests arriving together must not both find the last token. Redis runs a
// script to completion before serving anything else, so the read, the
// arithmetic, and the write are one step, the same property the confirm
// transaction gets from a row lock.
//
// It mirrors Bucket.Take exactly, including treating a clock that went backwards
// as no time passing. The shared contract suite is what keeps the two from
// drifting apart.
var takeScript = redis.NewScript(`
local burst = tonumber(ARGV[1])
local interval = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local held = redis.call('HMGET', KEYS[1], 'tokens', 'updated_at')
local tokens = tonumber(held[1])
local updated = tonumber(held[2])

if tokens == nil or updated == nil then
    tokens = burst
else
    local elapsed = now - updated
    if elapsed < 0 then
        elapsed = 0
    end

    tokens = tokens + (elapsed / interval)
end

if tokens > burst then
    tokens = burst
end

local allowed = 0
local wait = 0

if tokens >= 1 then
    allowed = 1
    tokens = tokens - 1
else
    wait = math.ceil((1 - tokens) * interval)
end

redis.call('HSET', KEYS[1], 'tokens', tokens, 'updated_at', now)
redis.call('PEXPIRE', KEYS[1], math.ceil(burst * interval) + interval)

return {allowed, math.floor(tokens), wait}
`)

// RedisLimiter is the shared implementation. It is what makes one limit mean one
// limit across every api instance.
type RedisLimiter struct {
    client redis.UniversalClient
}

// NewRedisLimiter wraps a client.
//
// Param:
// client - redis.UniversalClient (the connection, never nil)
//
// Return:
//   - the limiter
//   - ErrInvalidRequest when there is no client, refused here rather than as a
//     panic on the first request
func NewRedisLimiter(client redis.UniversalClient) (*RedisLimiter, error) {
    if client == nil {
        return nil, ErrInvalidRequest
    }

    return &RedisLimiter{client: client}, nil
}

// Allow applies one request to the bucket behind a key.
//
// Return:
//   - the decision, when Redis answered
//   - ErrUnavailable, when it did not. The caller decides what an unreachable
//     limiter means for its route, because failing open and failing closed are
//     both wrong somewhere
func (limiter *RedisLimiter) Allow(ctx context.Context, key string, bucket Bucket, now time.Time) (Decision, error) {
    if key == "" || !bucket.IsUsable() || now.IsZero() {
        return Decision{}, ErrInvalidRequest
    }

    answer, err := takeScript.Run(ctx, limiter.client, []string{key},
        bucket.Burst,
        bucket.Interval.Milliseconds(),
        now.UnixMilli()).Int64Slice()
    if err != nil {
        return Decision{}, ErrUnavailable
    }

    if len(answer) != 3 {
        return Decision{}, ErrUnavailable
    }

    if answer[0] != 1 {
        return Decision{
            Allowed:    false,
            Remaining:  0,
            RetryAfter: roundUpToSecond(time.Duration(answer[2]) * time.Millisecond),
        }, nil
    }

    return Decision{Allowed: true, Remaining: int(answer[1])}, nil
}

// roundUpToSecond matches what the pure arithmetic does with a wait.
//
// The script works in milliseconds because that is what a clock crosses a
// network as. A client is told whole seconds, and told at least one, because
// being told to wait zero seconds turns a refusal into a retry loop.
func roundUpToSecond(wait time.Duration) time.Duration {
    if remainder := wait % time.Second; remainder != 0 {
        wait += time.Second - remainder
    }

    if wait < time.Second {
        wait = time.Second
    }

    return wait
}
