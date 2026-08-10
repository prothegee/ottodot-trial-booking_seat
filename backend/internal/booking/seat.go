package booking

// LowestFreeSeat returns the lowest seat number in a class that no booking
// holds yet.
//
// This mirrors the generate_series query inside the confirm transaction on
// purpose. The sql version runs while the class row is locked and is the
// authority. This one is what the in-memory repository uses, and what lets the
// seat rule be read and tested without a database in front of it.
//
// Note:
//   - a seat that a cancelled booking used to hold is free again, because the
//     repository clears seat_no when it cancels. Nothing here has to know that.
//
// Param:
// capacity - int16 (how many seats the class has, seats are numbered from 1)
// taken - []int16 (seat numbers already held, in any order, duplicates ignored)
//
// Return:
//   - the lowest free seat and true
//   - 0 and false when every seat is taken, or when capacity is not positive
func LowestFreeSeat(capacity int16, taken []int16) (int16, bool) {
	if capacity < 1 {
		return 0, false
	}

	occupied := make(map[int16]struct{}, len(taken))

	for _, seat := range taken {
		occupied[seat] = struct{}{}
	}

	// The counter is an int rather than an int16 so a class at the top of the
	// smallint range cannot overflow the loop into a negative seat number.
	for seat := 1; seat <= int(capacity); seat++ {
		if _, found := occupied[int16(seat)]; !found {
			return int16(seat), true
		}
	}

	return 0, false
}
