package repository

import (
	"fmt"
	"reflect"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
)

// testTime is the fixed instant every repository test uses, so a scripted row
// and an expected struct compare deterministically.
var testTime = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

// timeNow returns the fixture instant.
func timeNow() time.Time { return testTime }

// testOrder returns an order with every field set, so a scan that forgets a
// column compares unequal and the test fails.
func testOrder() domain.Order {
	expiresAt := testTime.Add(30 * time.Minute)
	return domain.Order{
		ID:              "order-1",
		OrderNo:         "SP20261020ABCDEFGH",
		ParentAccountID: "parent-1",
		PlanID:          "plan-1",
		PlanCode:        "sprout_year",
		PlanDisplayName: "初芽年度陪伴",
		BillingPeriod:   domain.BillingPeriodYearly,
		PeriodMonths:    12,
		AIQuotaUSD:      "20.000000",
		UnitPriceCents:  19900,
		DiscountCents:   0,
		PayableCents:    19900,
		Status:          domain.OrderStatusPending,
		IdempotencyKey:  "key-1",
		ExpiresAt:       &expiresAt,
		ClientIP:        "127.0.0.1",
		Remark:          "",
		CreatedAt:       testTime,
		UpdatedAt:       testTime,
	}
}

// orderScanRow builds a row that fills exactly the columns orderColumns reads.
//
// The order of values mirrors the SELECT list, so a change to the projection
// that is not mirrored here makes the scan fail rather than pass with shifted
// values.
func orderScanRow(order domain.Order) scanRow {
	return scanRow{values: []any{
		order.ID,
		order.OrderNo,
		order.ParentAccountID,
		order.PlanID,
		order.PlanCode,
		order.PlanDisplayName,
		string(order.BillingPeriod),
		order.PeriodMonths,
		order.AIQuotaUSD,
		order.QuotaValidityDays,
		int64(order.UnitPriceCents),
		int64(order.DiscountCents),
		int64(order.PayableCents),
		int64(order.PaidCents),
		int64(order.RefundedCents),
		string(order.Status),
		order.IdempotencyKey,
		order.ExpiresAt,
		order.PaidAt,
		order.CancelledAt,
		order.ClosedAt,
		order.ClientIP,
		order.Remark,
		order.CreatedAt,
		order.UpdatedAt,
	}}
}

// testNotification returns a notification ledger row for insert tests.
func testNotification() domain.PaymentNotification {
	return domain.PaymentNotification{
		ID:                "notification-1",
		Channel:           domain.PaymentChannelWeChat,
		NotificationID:    "notify-1",
		PayloadDigest:     "sha256:deadbeef",
		SignatureVerified: true,
		Result:            domain.NotificationResultReceived,
		CreatedAt:         testTime,
		UpdatedAt:         testTime,
	}
}

// assignValue writes one scripted value into one pgx scan destination.
//
// The repository scans into pointers to concrete types, so supporting those is
// enough; anything else returns an error that names the type, which is what a
// test author needs to fix the script.
func assignValue(destination any, value any) error {
	switch target := destination.(type) {
	case *string:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("scripted row: cannot scan %T into *string", value)
		}
		*target = text
	case *int:
		number, ok := value.(int)
		if !ok {
			return fmt.Errorf("scripted row: cannot scan %T into *int", value)
		}
		*target = number
	case *int64:
		number, ok := value.(int64)
		if !ok {
			return fmt.Errorf("scripted row: cannot scan %T into *int64", value)
		}
		*target = number
	case *bool:
		flag, ok := value.(bool)
		if !ok {
			return fmt.Errorf("scripted row: cannot scan %T into *bool", value)
		}
		*target = flag
	case *time.Time:
		// The entity stores a nullable column as a plain *time.Time, so a nil
		// script means NULL and the zero time is the faithful representation.
		if value == nil {
			*target = time.Time{}
			return nil
		}
		instant, ok := value.(time.Time)
		if !ok {
			return fmt.Errorf("scripted row: cannot scan %T into *time.Time", value)
		}
		*target = instant
	case *[]byte:
		payload, ok := value.([]byte)
		if !ok {
			return fmt.Errorf("scripted row: cannot scan %T into *[]byte", value)
		}
		*target = payload
	default:
		// A JSON column scans into a type that decodes it; nullable columns scan
		// into a pointer to pointer. Try both before failing, so the error names
		// the real problem instead of a scan quirk.
		if decoder, ok := destination.(jsonTarget); ok {
			return assignJSONBytes(decoder, value)
		}
		if err := assignPointerPointer(destination, value); err == nil {
			return nil
		}
		return fmt.Errorf("scripted row: unsupported destination %T", destination)
	}
	return nil
}

// assignPointerPointer handles the pointer-to-pointer targets pgx uses for a
// nullable column, such as **time.Time and **string.
func assignPointerPointer(destination any, value any) error {
	switch target := destination.(type) {
	case **time.Time:
		// A fixture may hold the nullable column as *time.Time, so both the
		// pointed-to value and a nil pointer are accepted here.
		if value == nil {
			*target = nil
			return nil
		}
		if pointer, ok := value.(*time.Time); ok {
			if pointer == nil {
				*target = nil
				return nil
			}
			copied := *pointer
			*target = &copied
			return nil
		}
		instant, ok := value.(time.Time)
		if !ok {
			return fmt.Errorf("scripted row: cannot scan %T into **time.Time", value)
		}
		copied := instant
		*target = &copied
		return nil
	case *time.Time:
		// The entity keeps a nullable column as *time.Time, so a nil script
		// means the column is NULL.
		if value == nil {
			*target = time.Time{}
			return nil
		}
		instant, ok := value.(time.Time)
		if !ok {
			return fmt.Errorf("scripted row: cannot scan %T into *time.Time", value)
		}
		*target = instant
		return nil
	case **string:
		if value == nil {
			*target = nil
			return nil
		}
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("scripted row: cannot scan %T into **string", value)
		}
		copied := text
		*target = &copied
		return nil
	default:
		return fmt.Errorf("scripted row: unsupported nullable destination %T (%s)",
			destination, reflect.TypeOf(destination))
	}
}

// jsonTarget is a destination that accepts a JSON object body.
type jsonTarget interface {
	UnmarshalJSON(data []byte) error
}

// assignJSONBytes feeds scripted JSON into a destination that decodes it.
func assignJSONBytes(destination jsonTarget, value any) error {
	payload, ok := value.([]byte)
	if !ok {
		text, textOK := value.(string)
		if !textOK {
			return fmt.Errorf("scripted row: cannot decode %T as JSON", value)
		}
		payload = []byte(text)
	}
	return destination.UnmarshalJSON(payload)
}
