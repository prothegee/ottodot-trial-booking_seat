# Frontend Decision Records

Numbered records, each with the context that forced a choice, the choice itself,
and what living with it costs. Cross-stack decisions are recorded in
`../backend/ADR.md` and not repeated here.

| status | meaning |
| :- | :- |
| accepted | decided and implemented |
| planned | decided, implementation in a later phase |

<br>

## ADR-F001: SvelteKit with server rendering off, static output

**Status:** accepted, phase 1

**Context.** Every meaningful decision in this system happens inside a
transaction on the Go backend. A rendering server in front of it would be a
second moving part.

**Decision.** SvelteKit with `ssr = false` and `prerender = false`, built to a
static bundle with one fallback document.

**Consequences.** No server rendered first paint, and the client asks the api
for everything it shows. In exchange there is one backend rather than two, one
place where a seat count can be trusted, and a deployment that is a directory of
files.

Prerendering is off for the same reason: there is no page here whose content is
known before a parent signs in.

<br>

## ADR-F002: Caddy in its own container

**Status:** accepted, phase 1

**Context.** A static bundle needs something to serve it, and client routes such
as `/book/[classId]` only exist at runtime.

**Decision.** Caddy, with `try_files` handing the same document to every unknown
path.

**Alternatives rejected.** A Node process, which would mean a runtime to patch
for no benefit. A dev server in production, which is not what one is for.

**Consequences.** One more container, which the monitoring already covers.

<br>

## ADR-F003: Seat counts are advisory and never decide anything

**Status:** accepted, phase 1

**Context.** The client can show a seat count. The temptation is to use it, for
example by refusing to submit when it reads zero.

**Decision.** The count is a hint. The screen may grey out a full class to save
a wasted click, and every step still handles a rejection that contradicts what
it just showed.

**Consequences.** Every screen needs a path for an outcome it did not expect.
That is more work than trusting the number, and it is the only version that is
correct, because the number is stale the moment it is rendered.

<br>

## ADR-F004: The client never reads a token

**Status:** accepted, phase 2

**Context.** The access token is a JWT. Decoding it would give a display name
without a round trip.

**Decision.** Both tokens live in HttpOnly cookies. No code path reads, decodes,
or stores either one. Every request sends `credentials: "include"`. Whether a
session is valid is learned from a response.

**Alternatives rejected.** `localStorage`, which any script on the page can
read. Decoding the JWT for a display name, which would make the client depend on
a claim shape it does not own, for a name that one call to `/auth/me` returns.

**Consequences.** One extra round trip after signing in. In exchange there is no
token anywhere a script can reach, and the claim payload can stay free of
anything identifying, because nothing here needs to read it.

<br>

## ADR-F005: One refresh at a time, then exactly one retry

**Status:** accepted, phase 2

**Context.** Several calls can be in flight when the access token expires. The
naive fix retries each one after its own refresh.

**Decision.** A coordinator that collapses concurrent callers into a single
refresh call, then each caller retries its original request once. Never a loop.

**Consequences.** This is not an optimisation. The backend rotates the refresh
token on use and treats a second use of a rotated one as reuse, which revokes
the whole family. Five parallel refreshes would therefore end the session they
were trying to save.

A failed refresh reports the sign out once, not once per waiting caller, for the
same reason: three sign outs would mean three navigations away from whatever the
parent was looking at.

<br>

## ADR-F006: The api client reports a sign out, something else performs it

**Status:** accepted, phase 2

**Context.** The client is the only place a 401 can be noticed. It is also the
wrong place to know about routing.

**Decision.** `createApiClient` takes an `onSignOut` callback and does nothing
else about it. `lib/session/sign_out.ts` owns what a hard sign out does: clear
the store, clear session storage, route to sign in with a reason.
`lib/session/client.ts` is the one file that wires them together.

**Consequences.** One more indirection. In exchange the api client has no
opinion about navigation and is testable with no router, and the sign out is one
function rather than three lines repeated at every call site, which is how a
signed-out screen ends up still showing the previous parent's children.

<br>

## ADR-F007: Session storage is cleared wholesale on sign out

**Status:** accepted, phase 2

**Context.** The internal cache will write entries. Removing them key by key
means remembering to add each new key to the sign-out path.

**Decision.** `sessionStorage.clear()`.

**Consequences.** Anything else on the origin goes too, which is nothing, since
this client is the only writer. A key added in a later phase is covered without
anyone remembering, and a stale entry surviving a sign out is exactly what would
leak between two parents on a shared machine.

<br>

## ADR-F008: The auth store is memory only

**Status:** accepted, phase 2

**Context.** Mirroring the session into storage would survive a reload.

**Decision.** Memory only. A reload asks the api again.

**Consequences.** One call after a refresh of the page. In exchange there is no
durable copy of a parent's details anywhere in the browser, which matters
because the session itself lives in cookies this code cannot read.

The store also copies across only the fields it is allowed to hold. If the api
ever starts sending an email alongside the session, it stops there rather than
reaching a component. A test asserts that on the serialized state, because the
risk is a field nobody thought to look for.

<br>

## ADR-F009: Wire field names are used unchanged

**Status:** accepted, phase 2

**Context.** The api speaks snake_case. TypeScript convention is camelCase. The
usual answer is a mapping layer.

**Decision.** The api's own names, unchanged. `parent_id`, not `parentId`. No
second vocabulary and no mapper.

**Consequences.** snake_case appears in components, which reads oddly for a
TypeScript codebase. In exchange there is one name per field, a reviewer can
search the backend for the same word, and there is no layer to keep in step
when the contract changes.

<br>

## ADR-F010: Error text is rendered from the typed code

**Status:** accepted, phase 2

**Context.** The api sends a message alongside every error code.

**Decision.** The client maps the code to its own wording. The server's prose is
never pasted into the page.

**Consequences.** One mapping table to keep in step with the backend envelope.
In exchange the wording on screen is owned here, no server text can leak an
internal detail onto a recording, and an unknown code falls back to a generic
error rather than a blank page.

`internal_error` gets particular care: it is the one code that says nothing
about the booking, so the wording never claims the seat was lost or the payment
failed. Neither is known from here.

<br>

## ADR-F011: The fake transport is its own file

**Status:** accepted, phase 2

**Context.** The interface, the real transport, and the fake could sit together.

**Decision.** `transport.ts` holds the interface and the fetch transport.
`transport_fake.ts` holds the fake.

**Consequences.** One more file. In exchange test-only code can never reach a
production bundle, and the two cannot quietly grow into each other.

<br>

## ADR-F012: Tests use an injected fake, not a browser

**Status:** accepted, phase 1

**Context.** The alternatives are a mock service worker, a real network, or a
browser runner.

**Decision.** The api client takes a transport at construction and every test
passes a fake one. No network, no browser, no containers.

**Consequences.** The fake has to stay faithful to the api contract, which is a
real risk and is mitigated by the error mapping table being written out by hand
in its own test rather than read from the implementation.

In exchange the whole suite runs in about two seconds, and a test can assert
that no request was sent at all, which is the only way to prove the fresh-cache
case in the next phase.

<br>

## ADR-F013: A three tier client cache backed by session storage

**Status:** planned, phase 3

**Context.** A repeat class-list view should not cost a database read.

**Decision.** Fresh for five seconds, served from memory with no request at all.
Stale to thirty seconds, rendered at once and revalidated in the background with
`If-None-Match`. Cold after that.

**Consequences.** Invalidation has to be wired into every mutation and into sign
out. This is safe only because of ADR-F003: everything cacheable is advisory.

<br>

## ADR-F014: An unknown outcome reuses the idempotency key

**Status:** planned, phase 5

**Context.** A decline and an `internal_error` look similar on screen and are
opposite underneath. A decline means no money moved. An `internal_error` means
the client does not know.

**Decision.** A retry after `internal_error` resends the original idempotency
key. A retry after a decline gets a fresh one.

**Consequences.** The client has to tell two failures apart that look alike. In
exchange a parent is never charged twice for the same attempt.

<br>

## ADR-F015: Visual polish is the last phase

**Status:** planned, phase 9

**Context.** The brief states plainly that a polished interface is not required
and that the weight is on the data model, the backend logic, and the
explanation.

**Decision.** Layout and responsiveness come last, and only if time remains.

**Consequences.** The screens read plainly until then. The one exception is the
pair that appears in the video, payment failure and duplicate booking, which
have to be legible on a recording.
