package domain

import (
	"fmt"
	"math"
	"strings"
)

// Money represents a monetary amount in a specific currency. Constructing
// one always goes through NewMoney, so a negative amount or an
// unsupported currency can never enter the domain the way a bare float64
// silently could.
type Money struct {
	amount   float64
	currency string
}

// supportedCurrencies is deliberately small; extend as the business needs.
var supportedCurrencies = map[string]bool{
	"USD": true,
	"EUR": true,
	"GBP": true,
}

// NewMoney validates amount and currency and returns a Money value. Amount
// must be finite and non-negative; currency must be a known ISO 4217 code
// (case-insensitive on input, stored upper-case). Amount is rounded to 2
// decimal places (cents) so float drift doesn't accumulate across repeated
// persistence round-trips.
func NewMoney(amount float64, currency string) (Money, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return Money{}, fmt.Errorf("%w: amount is not a finite number", ErrInvalidMoney)
	}
	if amount < 0 {
		return Money{}, fmt.Errorf("%w: amount must be non-negative, got %v", ErrInvalidMoney, amount)
	}

	code := strings.ToUpper(strings.TrimSpace(currency))
	if !supportedCurrencies[code] {
		return Money{}, fmt.Errorf("%w: unsupported currency %q", ErrInvalidMoney, currency)
	}

	rounded := math.Round(amount*100) / 100
	return Money{amount: rounded, currency: code}, nil
}

// MustMoney is like NewMoney but panics on error. Reserved for tests and
// startup-time constants where the value is a known-valid literal.
func MustMoney(amount float64, currency string) Money {
	m, err := NewMoney(amount, currency)
	if err != nil {
		panic(err)
	}
	return m
}

func (m Money) Amount() float64  { return m.amount }
func (m Money) Currency() string { return m.currency }
func (m Money) String() string   { return fmt.Sprintf("%.2f %s", m.amount, m.currency) }
