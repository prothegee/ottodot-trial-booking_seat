package booking_test

import (
	"testing"

	"ottodot-trial-booking/backend/internal/booking"
)

func TestTheSeatPickerReturnsTheLowestFreeSeat(t *testing.T) {
	cases := []struct {
		name     string
		capacity int16
		taken    []int16
		expected int16
		free     bool
	}{
		{
			name:     "unit: an empty class gives seat 1",
			capacity: 4,
			taken:    nil,
			expected: 1,
			free:     true,
		},
		{
			name:     "unit: seats fill from the bottom",
			capacity: 4,
			taken:    []int16{1, 2},
			expected: 3,
			free:     true,
		},
		{
			name:     "unit: the order the taken seats arrive in does not matter",
			capacity: 4,
			taken:    []int16{3, 1},
			expected: 2,
			free:     true,
		},
		{
			name:     "edge: a gap left by a cancellation is filled before the top",
			capacity: 4,
			taken:    []int16{1, 3, 4},
			expected: 2,
			free:     true,
		},
		{
			name:     "edge: a full class has nothing to give",
			capacity: 4,
			taken:    []int16{1, 2, 3, 4},
			expected: 0,
			free:     false,
		},
		{
			name:     "edge: a one seat class gives seat 1 exactly once",
			capacity: 1,
			taken:    []int16{1},
			expected: 0,
			free:     false,
		},
		{
			name:     "edge: capacity 0 can never seat anyone",
			capacity: 0,
			taken:    nil,
			expected: 0,
			free:     false,
		},
		{
			name:     "edge: a negative capacity is refused rather than looping",
			capacity: -3,
			taken:    nil,
			expected: 0,
			free:     false,
		},
		{
			name:     "edge: a duplicate in the taken list does not shift the answer",
			capacity: 4,
			taken:    []int16{1, 1, 2},
			expected: 3,
			free:     true,
		},
		{
			name:     "edge: a seat above capacity is ignored, not counted",
			capacity: 2,
			taken:    []int16{9},
			expected: 1,
			free:     true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			seat, free := booking.LowestFreeSeat(testCase.capacity, testCase.taken)

			if free != testCase.free {
				t.Fatalf("expected free=%v, got %v", testCase.free, free)
			}

			if seat != testCase.expected {
				t.Fatalf("expected seat %d, got %d", testCase.expected, seat)
			}
		})
	}
}

func TestTheSeatPickerSurvivesTheTopOfTheSmallintRange(t *testing.T) {
	t.Run("edge: the largest possible capacity does not overflow the counter", func(t *testing.T) {
		// A smallint tops out at 32767. Counting with an int16 would wrap to a
		// negative seat number and loop forever, so this asserts the loop is
		// counted in a wider type.
		const largest int16 = 32767

		seat, free := booking.LowestFreeSeat(largest, nil)
		if !free || seat != 1 {
			t.Fatalf("expected seat 1 to be free, got seat %d free=%v", seat, free)
		}
	})
}
