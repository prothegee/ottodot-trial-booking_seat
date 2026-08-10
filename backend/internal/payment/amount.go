package payment

// DefaultCurrency matches the default on payment_attempts.currency. It is a
// value rather than something read per request, because this service prices one
// trial class in one country.
const DefaultCurrency = "SGD"

// currencyLength is the char(3) column, and the ISO 4217 code length it holds.
const currencyLength = 3

// Amount is money as the database stores it: whole cents and a currency code.
//
// Cents rather than a float, because a float cannot hold 0.10 and money that
// does not add up is worse than money that is awkward to read.
type Amount struct {
	// Cents is the charge in the smallest unit of the currency. It is int32
	// because the column is an integer, so a value that would not survive the
	// write cannot be built here either.
	Cents int32

	// Currency is the three letter code. An empty value means DefaultCurrency,
	// filled in by Normalised rather than assumed by every caller.
	Currency string
}

// Normalised fills in the currency this service defaults to, so a caller that
// only knows the price does not have to name the country.
func (amount Amount) Normalised() Amount {
	if amount.Currency == "" {
		amount.Currency = DefaultCurrency
	}

	return amount
}

// Validate refuses an amount before the driver ever sees it.
//
// The schema already carries `check (amount_cents > 0)`. That check is the
// backstop, not the message: a constraint violation reaches a caller as a
// database error nobody can act on, so the readable answer is produced here.
//
// Note:
//   - zero is refused as well as a negative. A free trial is a booking with no
//     payment at all, not a payment of nothing, and letting zero through would
//     put a settled row against a charge that never happened.
//
// Return:
//   - nil when the amount can be written and charged
//   - ErrInvalidAmount when the value is zero or below
//   - ErrInvalidCurrency when the code is not three capital letters
func (amount Amount) Validate() error {
	if amount.Cents <= 0 {
		return ErrInvalidAmount
	}

	currency := amount.Normalised().Currency

	if len(currency) != currencyLength {
		return ErrInvalidCurrency
	}

	for i := 0; i < len(currency); i++ {
		if currency[i] < 'A' || currency[i] > 'Z' {
			return ErrInvalidCurrency
		}
	}

	return nil
}

// SameAs reports whether two amounts are the one charge.
//
// It is what decides whether a replayed idempotency key is a retry or a second,
// different charge wearing the first one's key.
func (amount Amount) SameAs(other Amount) bool {
	left := amount.Normalised()
	right := other.Normalised()

	return left.Cents == right.Cents && left.Currency == right.Currency
}
