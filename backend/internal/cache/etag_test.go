package cache_test

import (
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/cache"
)

func TestBuildingATag(t *testing.T) {
    t.Run("unit: the same version and the same bytes produce the same tag", func(t *testing.T) {
        first := cache.BuildETag(41, []byte(`{"classes":[]}`))
        second := cache.BuildETag(41, []byte(`{"classes":[]}`))

        if first != second {
            t.Fatalf("the same input produced %s then %s, a client would revalidate forever", first, second)
        }
    })

    t.Run("unit: a tag is quoted, which is what makes it a strong validator", func(t *testing.T) {
        tag := cache.BuildETag(1, []byte("body"))

        if !strings.HasPrefix(tag, `"`) || !strings.HasSuffix(tag, `"`) {
            t.Fatalf("the tag %s is not quoted", tag)
        }
    })

    t.Run("unit: the version is carried in the tag, so it can be read back by eye", func(t *testing.T) {
        tag := cache.BuildETag(41, []byte("body"))

        if !strings.HasPrefix(tag, `"41-`) {
            t.Fatalf("the tag %s does not open with its version", tag)
        }
    })

    t.Run("edge: different bytes at the same version change the tag", func(t *testing.T) {
        before := cache.BuildETag(41, []byte(`{"seats_remaining":2}`))
        after := cache.BuildETag(41, []byte(`{"seats_remaining":1}`))

        if before == after {
            t.Fatal("a changed body kept its tag, so a stale seat count would be served as fresh")
        }
    })

    t.Run("edge: a version bump with identical bytes still changes the tag", func(t *testing.T) {
        before := cache.BuildETag(41, []byte(`{"classes":[]}`))
        after := cache.BuildETag(42, []byte(`{"classes":[]}`))

        if before == after {
            t.Fatal("the version was ignored, so an invalidation would be invisible to a client")
        }
    })

    t.Run("edge: an empty body still produces a usable tag", func(t *testing.T) {
        tag := cache.BuildETag(0, nil)

        if tag == "" || tag == `""` {
            t.Fatalf("an empty body produced %q, which no client can send back", tag)
        }
    })
}

func TestMatchingATag(t *testing.T) {
    tag := cache.BuildETag(41, []byte(`{"classes":[]}`))

    t.Run("unit: the tag a client holds matches the tag on hand", func(t *testing.T) {
        if !cache.ETagMatches(tag, tag) {
            t.Fatal("a client holding this exact representation was told it had changed")
        }
    })

    t.Run("unit: a different tag does not match", func(t *testing.T) {
        if cache.ETagMatches(cache.BuildETag(40, []byte(`{"classes":[]}`)), tag) {
            t.Fatal("an older tag matched, so a stale body would be kept")
        }
    })

    t.Run("edge: a list is searched, not compared whole", func(t *testing.T) {
        header := cache.BuildETag(39, []byte("a")) + ", " + tag + ", " + cache.BuildETag(40, []byte("b"))

        if !cache.ETagMatches(header, tag) {
            t.Fatal("a header carrying several tags was not searched")
        }
    })

    t.Run("edge: a weak marker added by a proxy still matches", func(t *testing.T) {
        if !cache.ETagMatches("W/"+tag, tag) {
            t.Fatal("a weakened tag was treated as different, which turns every request into a miss")
        }
    })

    t.Run("edge: a star matches anything, because it means any representation", func(t *testing.T) {
        if !cache.ETagMatches("*", tag) {
            t.Fatal("the wildcard was not honoured")
        }
    })

    t.Run("edge: an absent header matches nothing", func(t *testing.T) {
        if cache.ETagMatches("", tag) {
            t.Fatal("a first request with no header was answered 304, so it would render nothing")
        }
    })

    t.Run("edge: an absent tag matches nothing, even against a star", func(t *testing.T) {
        if cache.ETagMatches("*", "") {
            t.Fatal("a response with no tag claimed a client already held it")
        }
    })
}
