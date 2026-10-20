package domain

import (
	"errors"
	"testing"
)

// validLedgerEntries returns one entry of each type that must pass validation.
func validLedgerEntries() []QuotaLedgerEntry {
	return []QuotaLedgerEntry{
		{
			ParentAccountID: "family-1",
			EntryType:       QuotaEntryPurchaseGrant,
			AmountUSD:       "5",
			BalanceAfterUSD: "5",
			OrderID:         "order-1",
		},
		{
			ParentAccountID: "family-1",
			EntryType:       QuotaEntryAdminAdjustment,
			AmountUSD:       "-1.5",
			BalanceAfterUSD: "3.5",
			Reason:          "客服补偿误扣",
		},
		{
			ParentAccountID: "family-1",
			EntryType:       QuotaEntryProviderSync,
			AmountUSD:       "-0.25",
			BalanceAfterUSD: "3.25",
			// 3.25 - 0.25 = 3.00 observed by the provider.
			ObservedProviderBalanceUSD: "3",
			Detail:                     map[string]any{"kind": "consumption"},
		},
		{
			ParentAccountID:            "family-1",
			EntryType:                  QuotaEntryProviderSync,
			AmountUSD:                  "0.5",
			BalanceAfterUSD:            "3.75",
			ObservedProviderBalanceUSD: "4.25",
			Detail:                     map[string]any{"kind": "drift"},
		},
		{
			ParentAccountID: "family-1",
			EntryType:       QuotaEntryRefundClawback,
			AmountUSD:       "-2",
			BalanceAfterUSD: "1.75",
			RefundID:        "refund-1",
		},
		{
			ParentAccountID: "family-1",
			EntryType:       QuotaEntryExpiry,
			AmountUSD:       "-1.75",
			BalanceAfterUSD: "0",
		},
	}
}

func TestValidateQuotaLedgerEntryAcceptsEveryType(t *testing.T) {
	t.Parallel()

	for _, entry := range validLedgerEntries() {
		if err := ValidateQuotaLedgerEntry(entry); err != nil {
			t.Fatalf("entry type %q rejected: %v", entry.EntryType, err)
		}
	}
}

func TestValidateQuotaLedgerEntryRejections(t *testing.T) {
	t.Parallel()

	grant := validLedgerEntries()[0]
	adjustment := validLedgerEntries()[1]
	syncEntry := validLedgerEntries()[2]

	cases := []struct {
		name  string
		entry QuotaLedgerEntry
	}{
		{name: "missing parent account", entry: withField(grant, func(entry *QuotaLedgerEntry) { entry.ParentAccountID = "" })},
		{name: "unknown entry type", entry: withField(grant, func(entry *QuotaLedgerEntry) { entry.EntryType = "bonus" })},
		{name: "empty amount", entry: withField(grant, func(entry *QuotaLedgerEntry) { entry.AmountUSD = "" })},
		{name: "non numeric amount", entry: withField(grant, func(entry *QuotaLedgerEntry) { entry.AmountUSD = "five" })},
		{name: "exponent amount", entry: withField(grant, func(entry *QuotaLedgerEntry) { entry.AmountUSD = "1e3" })},
		{name: "too many decimals", entry: withField(grant, func(entry *QuotaLedgerEntry) { entry.AmountUSD = "1.0000001" })},
		{name: "zero amount", entry: withField(grant, func(entry *QuotaLedgerEntry) { entry.AmountUSD = "0" })},
		{name: "zero amount with decimals", entry: withField(grant, func(entry *QuotaLedgerEntry) { entry.AmountUSD = "0.000000" })},
		{name: "unparseable balance", entry: withField(grant, func(entry *QuotaLedgerEntry) { entry.BalanceAfterUSD = "x" })},
		{name: "negative balance after", entry: withField(grant, func(entry *QuotaLedgerEntry) {
			entry.AmountUSD = "-1"
			entry.BalanceAfterUSD = "-1"
			entry.OrderID = ""
		})},
		{name: "grant removing quota", entry: withField(grant, func(entry *QuotaLedgerEntry) { entry.AmountUSD = "-1" })},
		{name: "clawback adding quota", entry: withField(validLedgerEntries()[4], func(entry *QuotaLedgerEntry) {
			entry.AmountUSD = "2"
		})},
		{name: "expiry adding quota", entry: withField(validLedgerEntries()[5], func(entry *QuotaLedgerEntry) {
			entry.AmountUSD = "1"
		})},
		{name: "adjustment without reason", entry: withField(adjustment, func(entry *QuotaLedgerEntry) { entry.Reason = "   " })},
		{name: "grant without order", entry: withField(grant, func(entry *QuotaLedgerEntry) { entry.OrderID = "" })},
		{name: "clawback without refund", entry: withField(validLedgerEntries()[4], func(entry *QuotaLedgerEntry) {
			entry.RefundID = ""
		})},
		{name: "observed balance on a non-sync entry", entry: withField(grant, func(entry *QuotaLedgerEntry) {
			entry.ObservedProviderBalanceUSD = "5"
		})},
		{name: "sync without observed balance", entry: withField(syncEntry, func(entry *QuotaLedgerEntry) {
			entry.ObservedProviderBalanceUSD = ""
		})},
		{name: "sync with negative observed balance", entry: withField(syncEntry, func(entry *QuotaLedgerEntry) {
			entry.ObservedProviderBalanceUSD = "-1"
		})},
		// The recorded delta must reproduce the observed balance; here the entry
		// claims 3.25 - 0.25 but reports 9 observed.
		{name: "sync delta disagrees with observed balance", entry: withField(syncEntry, func(entry *QuotaLedgerEntry) {
			entry.ObservedProviderBalanceUSD = "9"
		})},
		{name: "sync sign disagrees with observed balance", entry: withField(syncEntry, func(entry *QuotaLedgerEntry) {
			entry.AmountUSD = "0.25"
		})},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateQuotaLedgerEntry(testCase.entry); !errors.Is(err, ErrInvalidQuotaLedgerEntry) {
				t.Fatalf("error = %v, want ErrInvalidQuotaLedgerEntry", err)
			}
		})
	}
}

func TestQuotaLedgerEntryDirection(t *testing.T) {
	t.Parallel()

	for _, entry := range validLedgerEntries() {
		canAdd, canRemove := entry.EntryType.AllowedDirection()
		switch entry.Sign() {
		case 1:
			if !canAdd {
				t.Fatalf("%q must not add quota", entry.EntryType)
			}
		case -1:
			if !canRemove {
				t.Fatalf("%q must not remove quota", entry.EntryType)
			}
		default:
			t.Fatalf("%q has an unexpected zero sign", entry.EntryType)
		}
		if entry.NeedsBalanceCheck() != (entry.Sign() < 0) {
			t.Fatalf("%q NeedsBalanceCheck = %v, sign = %d", entry.EntryType, entry.NeedsBalanceCheck(), entry.Sign())
		}
	}
	if (QuotaLedgerEntry{AmountUSD: "x"}).Sign() != 0 {
		t.Fatal("unparseable amount must have sign 0")
	}
	if QuotaLedgerEntryType("bonus").IsKnown() {
		t.Fatal("unknown entry type reported as known")
	}
	for _, entryType := range QuotaLedgerEntryTypes {
		if !entryType.IsKnown() {
			t.Fatalf("declared entry type %q reported as unknown", entryType)
		}
	}
	if len(QuotaLedgerEntryTypes) != 5 {
		t.Fatalf("entry type count = %d, want 5", len(QuotaLedgerEntryTypes))
	}
}

func TestVerifyQuotaChain(t *testing.T) {
	t.Parallel()

	entries := validLedgerEntries()
	if err := VerifyQuotaChain("0", entries); err != nil {
		t.Fatalf("valid chain rejected: %v", err)
	}

	tampered := append([]QuotaLedgerEntry(nil), entries...)
	tampered[1].BalanceAfterUSD = "3.4"
	if err := VerifyQuotaChain("0", tampered); !errors.Is(err, ErrInvalidQuotaLedgerEntry) {
		t.Fatalf("tampered chain error = %v, want ErrInvalidQuotaLedgerEntry", err)
	}

	if err := VerifyQuotaChain("0", entries[1:]); !errors.Is(err, ErrInvalidQuotaLedgerEntry) {
		t.Fatalf("missing opening grant error = %v, want ErrInvalidQuotaLedgerEntry", err)
	}
	if err := VerifyQuotaChain("bad", nil); !errors.Is(err, ErrInvalidQuotaLedgerEntry) {
		t.Fatalf("bad opening balance error = %v, want ErrInvalidQuotaLedgerEntry", err)
	}
}

func TestValidateQuotaBalance(t *testing.T) {
	t.Parallel()

	clawback := validLedgerEntries()[4]
	if err := ValidateQuotaBalance(clawback, "3.75"); err != nil {
		t.Fatalf("affordable clawback rejected: %v", err)
	}
	// Removing more than the provider pool holds would leave a negative pool.
	if err := ValidateQuotaBalance(clawback, "1"); !errors.Is(err, ErrInvalidQuotaLedgerEntry) {
		t.Fatalf("overdraw error = %v, want ErrInvalidQuotaLedgerEntry", err)
	}
	if err := ValidateQuotaBalance(clawback, "n/a"); !errors.Is(err, ErrInvalidQuotaLedgerEntry) {
		t.Fatalf("bad balance error = %v, want ErrInvalidQuotaLedgerEntry", err)
	}
}

func TestQuotaParsingBoundaries(t *testing.T) {
	t.Parallel()

	accepted := map[string]string{
		"0":                    "0",
		"0.000001":             "0.000001",
		"1":                    "1",
		"1.5":                  "1.5",
		"3.250000":             "3.25",
		"-2":                   "-2",
		"-0.000001":            "-0.000001",
		"+5":                   "5",
		"9223372036854.775807": "9223372036854.775807",
	}
	for input, want := range accepted {
		parsed, err := parseQuota(input)
		if err != nil {
			t.Fatalf("parseQuota(%q) error = %v", input, err)
		}
		if got := parsed.FormatUSD(); got != want {
			t.Fatalf("parseQuota(%q).FormatUSD() = %q, want %q", input, got, want)
		}
	}

	rejected := []string{"", " ", "-", "+", ".", ".5", "5.a", "1.0000001", "abc", "1e3", "0x10", "9223372036854.775808", "9223372036855", "--1"}
	for _, input := range rejected {
		if _, err := parseQuota(input); err == nil {
			t.Fatalf("parseQuota(%q) accepted, want rejection", input)
		}
	}
}

func withField(entry QuotaLedgerEntry, mutate func(entry *QuotaLedgerEntry)) QuotaLedgerEntry {
	clone := entry
	if clone.Detail != nil {
		detail := make(map[string]any, len(clone.Detail))
		for key, value := range clone.Detail {
			detail[key] = value
		}
		clone.Detail = detail
	}
	mutate(&clone)
	return clone
}
