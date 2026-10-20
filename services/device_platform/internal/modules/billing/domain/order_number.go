package domain

import (
	"crypto/rand"
	"fmt"
	"time"
)

// orderNumberPrefix marks a platform order number in guardian-facing copy and
// support searches.
const orderNumberPrefix = "SP"

// refundNumberPrefix marks a refund number, which shares the same generator and
// segment shape.
const refundNumberPrefix = "SR"

// base32Alphabet is the RFC 4648 alphabet without padding. It holds no digits,
// so a number read aloud over a support call or typed by hand never mixes a
// letter with a similar-looking digit such as O/0 or I/1.
const base32Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

// orderNumberDateLayout is the local calendar day embedded in the number.
const orderNumberDateLayout = "20060102"

// orderNumberRandomLength is how many base32 characters follow the date.
//
// Eight characters carry 40 bits. The design doc's six-character sketch carries
// only 30 bits, which reaches roughly a 1% collision chance after 4,500
// same-day orders and near certainty at 10,000 — too weak for a number that is
// customer-visible, so the segment is widened and the database CHECK in
// migration 0073 matches it.
const orderNumberRandomLength = 8

// NewOrderNumber returns a collision-resistant order number such as
// SP20261020A1B2C3D4 for the local day of now.
//
// The random segment comes from crypto/rand, not math/rand: a predictable or
// seeded generator would let two replicas mint the same number under load.
// Even at 40 bits a collision cannot be ruled out, so the unique index on
// order_no stays the final authority and a caller must retry on its violation.
func NewOrderNumber(now time.Time) string {
	return newNumberWithPrefix(orderNumberPrefix, now)
}

// NewRefundNumber returns a refund number such as SR20261020A1B2C3D4 with the
// same collision resistance as NewOrderNumber.
func NewRefundNumber(now time.Time) string {
	return newNumberWithPrefix(refundNumberPrefix, now)
}

// newNumberWithPrefix builds prefix + YYYYMMDD + random base32 segment.
//
// A failing crypto/rand means the process cannot be trusted to mint unique
// identifiers at all, so the failure panics rather than returning a predictable
// number that could collide with a real order.
func newNumberWithPrefix(prefix string, now time.Time) string {
	var buffer [orderNumberRandomLength]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		panic(fmt.Sprintf("billing: generate order number: %v", err))
	}
	for index, value := range buffer {
		// 256 is an exact multiple of 32, so masking keeps the draw uniform.
		buffer[index] = base32Alphabet[value&0x1f]
	}
	return prefix + now.Format(orderNumberDateLayout) + string(buffer[:])
}
