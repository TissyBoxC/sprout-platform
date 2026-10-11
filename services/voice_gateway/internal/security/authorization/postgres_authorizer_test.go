package authorization

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// stubRow is the three-column row the authorizer scans. It mirrors the SELECT
// list (account status, child-data grant, AI-interaction grant).
type stubRow struct {
	status        string
	childData     bool
	aiInteraction bool
	err           error
}

func (row stubRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*(dest[0].(*string)) = row.status
	*(dest[1].(*bool)) = row.childData
	*(dest[2].(*bool)) = row.aiInteraction
	return nil
}

// recordingQuerier captures the SQL text so a test can prove the query reads
// both consent types, not just the one it happens to return grants for.
type recordingQuerier struct {
	sql string
	row pgx.Row
}

func (q *recordingQuerier) QueryRow(
	_ context.Context,
	sql string,
	_ ...any,
) pgx.Row {
	q.sql = sql
	return q.row
}

func TestAuthorizeDeviceRequiresBothConsentTypes(t *testing.T) {
	testCases := []struct {
		name          string
		status        string
		childData     bool
		aiInteraction bool
		wantErr       bool
	}{
		{name: "both granted", status: "active", childData: true, aiInteraction: true},
		{name: "child data withdrawn", status: "active", childData: false, aiInteraction: true, wantErr: true},
		{name: "ai interaction withdrawn", status: "active", childData: true, aiInteraction: false, wantErr: true},
		{name: "disabled account", status: "disabled", childData: true, aiInteraction: true, wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			querier := &recordingQuerier{row: stubRow{
				status:        testCase.status,
				childData:     testCase.childData,
				aiInteraction: testCase.aiInteraction,
			}}
			authorizer := &PostgresDeviceAuthorizer{queryer: querier}

			err := authorizer.AuthorizeDevice(context.Background(), "device-1")
			if testCase.wantErr && !errors.Is(err, ErrNotAuthorized) {
				t.Fatalf("AuthorizeDevice() error = %v, want ErrNotAuthorized", err)
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("AuthorizeDevice() error = %v, want nil", err)
			}
			for _, consentType := range voiceConsentTypes {
				if !strings.Contains(querier.sql, consentType) {
					t.Fatalf("query %q does not check consent type %q", querier.sql, consentType)
				}
			}
		})
	}
}

func TestAuthorizeDeviceFailsClosedWhenUnbound(t *testing.T) {
	querier := &recordingQuerier{row: stubRow{err: pgx.ErrNoRows}}
	authorizer := &PostgresDeviceAuthorizer{queryer: querier}
	if err := authorizer.AuthorizeDevice(context.Background(), "device-1"); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("unbound device error = %v, want ErrNotAuthorized", err)
	}
}

func TestAuthorizeDeviceFailsClosedWithoutDatabase(t *testing.T) {
	authorizer := &PostgresDeviceAuthorizer{}
	if err := authorizer.AuthorizeDevice(context.Background(), "device-1"); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("no-database error = %v, want ErrNotAuthorized", err)
	}
}
