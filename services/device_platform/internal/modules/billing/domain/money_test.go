package domain

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestCentsFormat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		amount Cents
		want   string
	}{
		{name: "zero", amount: 0, want: "¥0.00"},
		{name: "nine yuan ninety", amount: 990, want: "¥9.90"},
		{name: "large", amount: 12345, want: "¥123.45"},
		{name: "negative", amount: -990, want: "-¥9.90"},
		{name: "one fen", amount: 1, want: "¥0.01"},
		{name: "ten fen carries", amount: 10, want: "¥0.10"},
		{name: "negative one fen", amount: -1, want: "-¥0.01"},
		{name: "one yuan", amount: 100, want: "¥1.00"},
		{name: "negative zero is zero", amount: 0, want: "¥0.00"},
		{name: "max int64", amount: Cents(math.MaxInt64), want: "¥92233720368547758.07"},
		{name: "min int64", amount: Cents(math.MinInt64), want: "-¥92233720368547758.08"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := testCase.amount.Format(); got != testCase.want {
				t.Fatalf("Cents(%d).Format() = %q, want %q", int64(testCase.amount), got, testCase.want)
			}
			if got := testCase.amount.String(); got != testCase.want {
				t.Fatalf("Cents(%d).String() = %q, want %q", int64(testCase.amount), got, testCase.want)
			}
		})
	}
}

func TestParseCentsAccepts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  Cents
	}{
		{input: "9.90", want: 990},
		{input: "¥9.90", want: 990},
		{input: " 9.90 ", want: 990},
		{input: "0.00", want: 0},
		{input: "0", want: 0},
		{input: "0.0", want: 0},
		{input: "0.1", want: 10},
		{input: "1.5", want: 150},
		{input: "123.45", want: 12345},
		// A bare integer is yuan, matching the "¥9.90" form the field edits.
		{input: "990", want: 99000},
		{input: "-9.90", want: -990},
		{input: "-¥9.90", want: -990},
		{input: "¥0.01", want: 1},
		{input: "10.", want: 1000},
		{input: ".5", want: 50},
		{input: "¥.5", want: 50},
		{input: "0009.90", want: 990},
		{input: "92233720368547758.07", want: Cents(math.MaxInt64)},
		{input: "-92233720368547758.08", want: Cents(math.MinInt64)},
	}
	for _, testCase := range cases {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseCents(testCase.input)
			if err != nil {
				t.Fatalf("ParseCents(%q) error = %v", testCase.input, err)
			}
			if got != testCase.want {
				t.Fatalf("ParseCents(%q) = %d, want %d", testCase.input, int64(got), int64(testCase.want))
			}
		})
	}
}

func TestParseCentsRejects(t *testing.T) {
	t.Parallel()

	rejected := []string{
		"",
		"   ",
		"¥",
		"-",
		"-¥",
		"+9.90",
		"--9.90",
		"¥-9.90",
		"abc",
		"9.9a",
		"9.909",
		"0.001",
		"9.9.9",
		"¥ 9.90",
		"-.",
		"9 90",
		"1e3",
		"NaN",
		"Inf",
		".",
		"92233720368547758.08",
		"92233720368547759",
		"99999999999999999999999",
		"-92233720368547758.09",
	}
	for _, input := range rejected {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseCents(input)
			if err == nil {
				t.Fatalf("ParseCents(%q) = %d, want error", input, int64(got))
			}
			if !errors.Is(err, ErrInvalidAmount) {
				t.Fatalf("ParseCents(%q) error = %v, want ErrInvalidAmount", input, err)
			}
		})
	}
}

func TestCentsFromYuanStringRejectsNegative(t *testing.T) {
	t.Parallel()

	if got, err := CentsFromYuanString("19.90"); err != nil || got != 1990 {
		t.Fatalf("CentsFromYuanString(19.90) = %d, %v; want 1990, nil", int64(got), err)
	}
	if _, err := CentsFromYuanString("-19.90"); !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("negative yuan error = %v, want ErrInvalidAmount", err)
	}
	if _, err := CentsFromYuanString("x"); !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("non-numeric yuan error = %v, want ErrInvalidAmount", err)
	}
}

func TestCentsJSONUsesIntegerCents(t *testing.T) {
	t.Parallel()

	type payload struct {
		AmountCents Cents `json:"amount_cny_cents"`
	}
	encoded, err := json.Marshal(payload{AmountCents: 990})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != `{"amount_cny_cents":990}` {
		t.Fatalf("encoded = %s, want integer cent count", encoded)
	}

	var decoded payload
	if err := json.Unmarshal([]byte(`{"amount_cny_cents":-12345}`), &decoded); err != nil {
		t.Fatalf("unmarshal integer: %v", err)
	}
	if decoded.AmountCents != -12345 {
		t.Fatalf("decoded = %d, want -12345", int64(decoded.AmountCents))
	}

	for _, rejected := range []string{
		`{"amount_cny_cents":9.90}`,
		`{"amount_cny_cents":"9.90"}`,
		`{"amount_cny_cents":null}`,
		`{"amount_cny_cents":99999999999999999999}`,
	} {
		var target payload
		if err := json.Unmarshal([]byte(rejected), &target); !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("unmarshal %s error = %v, want ErrInvalidAmount", rejected, err)
		}
	}
}

func TestPercentOffRoundsHalfUpInIntegerMath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		total    Cents
		permille int
		want     Cents
	}{
		{name: "zero total", total: 0, permille: 250, want: 0},
		{name: "zero permille", total: 1000, permille: 0, want: 0},
		{name: "negative permille", total: 1000, permille: -250, want: 0},
		{name: "negative total", total: -1000, permille: 250, want: 0},
		{name: "full hundred percent", total: 990, permille: 1000, want: 990},
		{name: "over hundred percent clamps", total: 990, permille: 5000, want: 990},
		// 1 * 500 / 1000 = 0.5, rounded half up to 1.
		{name: "half fen rounds up", total: 1, permille: 500, want: 1},
		// 1 * 499 / 1000 = 0.499, rounded down to 0.
		{name: "just below half rounds down", total: 1, permille: 499, want: 0},
		// 9900 * 250 / 1000 = 2475 exactly.
		{name: "quarter of 99 yuan", total: 9900, permille: 250, want: 2475},
		// 990 * 250 / 1000 = 247.5 -> 248.
		{name: "quarter of nine ninety", total: 990, permille: 250, want: 248},
		// 1990 * 150 / 1000 = 298.5 -> 299.
		{name: "fifteen percent of nineteen ninety", total: 1990, permille: 150, want: 299},
		// 100 * 333 / 1000 = 33.3 -> 33.
		{name: "33.3 percent of one yuan", total: 100, permille: 333, want: 33},
		// 300 * 10 / 1000 = 3 exactly.
		{name: "ten permille of three yuan", total: 300, permille: 10, want: 3},
		// A very large total must not overflow before the division:
		// 1e11 * 250 = 2.5e13 fits, and stays exact.
		{name: "huge total stays exact", total: Cents(100_000_000_000), permille: 250, want: Cents(25_000_000_000)},
		// 9_223_372_036_854_000 * 999 / 1000 = 9_214_148_664_817_146 exactly.
		{name: "odd permille of huge total", total: Cents(9_223_372_036_854_000), permille: 999, want: Cents(9_214_148_664_817_146)},
		// Near max int64: max * 999 = 9_214_148_664_817_921_031, which overflows
		// int64, so this exercises the divide-first path and must stay exact.
		{name: "near max int64", total: Cents(math.MaxInt64), permille: 999, want: Cents(9_214_148_664_817_921_031)},
		{name: "max int64 full discount", total: Cents(math.MaxInt64), permille: 1000, want: Cents(math.MaxInt64)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := PercentOff(testCase.total, testCase.permille)
			if got != testCase.want {
				t.Fatalf(
					"PercentOff(%d, %d) = %d, want %d",
					int64(testCase.total),
					testCase.permille,
					int64(got),
					int64(testCase.want),
				)
			}
		})
	}
}

func TestPercentOffDiscountLeavesValidOrderAmounts(t *testing.T) {
	t.Parallel()

	for _, unitPrice := range []Cents{1, 7, 99, 990, 1990, 19900, 36500} {
		for _, permille := range []int{0, 1, 50, 100, 250, 333, 500, 750, 999, 1000} {
			discount := PercentOff(unitPrice, permille)
			if discount < 0 || discount > unitPrice {
				t.Fatalf("PercentOff(%d, %d) = %d out of range", int64(unitPrice), permille, int64(discount))
			}
			order := Order{
				UnitPriceCents: unitPrice,
				DiscountCents:  discount,
				PayableCents:   unitPrice - discount,
			}
			if err := ValidateOrderAmounts(order); err != nil {
				t.Fatalf("discount %d on %d rejected: %v", int64(discount), int64(unitPrice), err)
			}
		}
	}
}

func TestCentsAddSubOverflow(t *testing.T) {
	t.Parallel()

	if sum, err := Cents(1).Add(2); err != nil || sum != 3 {
		t.Fatalf("1 + 2 = %d, %v", int64(sum), err)
	}
	if _, err := Cents(math.MaxInt64).Add(1); !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("max + 1 error = %v, want ErrInvalidAmount", err)
	}
	if _, err := Cents(math.MinInt64).Sub(1); !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("min - 1 error = %v, want ErrInvalidAmount", err)
	}
	if difference, err := Cents(1).Sub(Cents(math.MaxInt64)); err != nil ||
		difference != Cents(math.MinInt64)+2 {
		t.Fatalf("1 - max = %d, %v", int64(difference), err)
	}
}
