package queue_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/queue"
)

// memoryFixture points the shared contract at the fake.
type memoryFixture struct {
	queue *queue.MemoryQueue
}

func newMemoryFixture(_ *testing.T) queueFixture {
	return &memoryFixture{queue: queue.NewMemoryQueue()}
}

func (fixture *memoryFixture) Queue() queue.Queue {
	return fixture.queue
}

func TestTheMemoryQueueHonoursTheContract(t *testing.T) {
	runQueueContract(t, newMemoryFixture)
}

func TestTheMemoryQueueIsSafeToShare(t *testing.T) {
	t.Run("integration: parallel polls hand each job to one caller only", func(t *testing.T) {
		// The same shape as the real-database proof, and worth running here for
		// a different reason: it is what catches a missing lock in the fake. It
		// proves nothing about SKIP LOCKED, because there is no transaction
		// here, which is why the proof tier runs the same shape for real.
		target := queue.NewMemoryQueue()

		const jobs = 6

		for written := 0; written < jobs; written++ {
			mustEnqueue(t, target, queue.KindExpireHold, contractMoment)
		}

		const pollers = 8

		var (
			waitGroup sync.WaitGroup
			mutex     sync.Mutex
			handedOut []string
		)

		for poller := 0; poller < pollers; poller++ {
			waitGroup.Add(1)

			go func() {
				defer waitGroup.Done()

				leased, err := target.Claim(context.Background(), queue.ClaimRequest{
					Now: contractMoment, Lease: contractLease, Limit: jobs, MaxAttempts: contractMaxAttempts,
				})

				mutex.Lock()
				defer mutex.Unlock()

				if err != nil {
					t.Errorf("unexpected failure from a parallel poll: %v", err)

					return
				}

				for _, job := range leased {
					handedOut = append(handedOut, job.ID)
				}
			}()
		}

		waitGroup.Wait()

		if len(handedOut) != jobs {
			t.Fatalf("expected every job handed out exactly once, got %d for %d jobs", len(handedOut), jobs)
		}

		seen := make(map[string]struct{}, len(handedOut))

		for _, jobID := range handedOut {
			if _, twice := seen[jobID]; twice {
				t.Fatalf("job %s was handed to two callers", jobID)
			}

			seen[jobID] = struct{}{}
		}
	})
}

func TestTheMemoryQueueRefusesAnIncompleteRequest(t *testing.T) {
	t.Run("edge: a write with no id is refused", func(t *testing.T) {
		target := queue.NewMemoryQueue()

		payload, err := queue.EncodeBookingPayload(payloadBooking)
		if err != nil {
			t.Fatalf("cannot encode a payload: %v", err)
		}

		_, err = target.Enqueue(context.Background(), queue.EnqueueRequest{
			Kind: queue.KindExpireHold, Payload: payload, Now: contractMoment,
		})
		if !errors.Is(err, queue.ErrInvalidRequest) {
			t.Fatalf("expected ErrInvalidRequest, got: %v", err)
		}
	})

	t.Run("edge: a write with no clock is refused, because the row would carry no age", func(t *testing.T) {
		target := queue.NewMemoryQueue()

		request := enqueueRequestFor(t, queue.KindExpireHold, contractMoment)
		request.Now = time.Time{}

		if _, err := target.Enqueue(context.Background(), request); !errors.Is(err, queue.ErrInvalidRequest) {
			t.Fatalf("expected ErrInvalidRequest, got: %v", err)
		}
	})
}

func TestTheMemoryQueueKeepsTheCallersPayloadOutOfItsHands(t *testing.T) {
	t.Run("edge: rewriting the buffer after enqueuing does not rewrite the job", func(t *testing.T) {
		// The sql version cannot be reached this way, because the bytes are
		// copied into the database on the way in. The fake has to match, or a
		// test that reuses a buffer passes against one and fails against the
		// other.
		target := queue.NewMemoryQueue()

		request := enqueueRequestFor(t, queue.KindExpireHold, contractMoment)

		written, err := target.Enqueue(context.Background(), request)
		if err != nil {
			t.Fatalf("expected the job to be written, got: %v", err)
		}

		for index := range request.Payload {
			request.Payload[index] = 'x'
		}

		if _, err := queue.DecodeBookingPayload(written.Payload); err != nil {
			t.Fatalf("expected the stored payload to be untouched, got: %v", err)
		}
	})
}
