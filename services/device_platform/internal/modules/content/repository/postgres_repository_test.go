package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestThemePackTransitionRejectsUnsupportedAction(t *testing.T) {
	for _, action := range []domain.Action{
		domain.ActionSubmit,
		domain.ActionApprove,
		domain.ActionReject,
		domain.ActionArchive,
		domain.Action("unknown"),
	} {
		t.Run(string(action), func(t *testing.T) {
			_, _, err := themePackTransition(action)
			if !errors.Is(err, domain.ErrThemeTransitionInvalid) {
				t.Fatalf("error = %v, want ErrThemeTransitionInvalid", err)
			}
		})
	}
}

func TestThemePackTransitionMapsOnlyDraftToPublishedAndPublishedToWithdrawn(t *testing.T) {
	cases := []struct {
		action domain.Action
		source domain.ThemePackStatus
		target domain.ThemePackStatus
	}{
		{
			action: domain.ActionPublish,
			source: domain.ThemePackDraft,
			target: domain.ThemePackPublished,
		},
		{
			action: domain.ActionWithdraw,
			source: domain.ThemePackPublished,
			target: domain.ThemePackWithdrawn,
		},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.action), func(t *testing.T) {
			source, target, err := themePackTransition(testCase.action)
			if err != nil {
				t.Fatalf("transition mapping: %v", err)
			}
			if source != testCase.source || target != testCase.target {
				t.Fatalf(
					"mapping = %s -> %s, want %s -> %s",
					source,
					target,
					testCase.source,
					testCase.target,
				)
			}
		})
	}
}

func TestTransitionThemePackRejectsUnchangedSourceWithStableError(t *testing.T) {
	executor := &themePackTransitionExecutor{
		tag:    pgconn.NewCommandTag("UPDATE 0"),
		status: domain.ThemePackWithdrawn,
	}
	_, err := transitionThemePack(
		context.Background(),
		executor,
		"theme_ocean",
		domain.ThemePackDraft,
		domain.ThemePackPublished,
		time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC),
	)
	if !errors.Is(err, domain.ErrThemeTransitionInvalid) {
		t.Fatalf("error = %v, want ErrThemeTransitionInvalid", err)
	}
	if executor.execCalls != 1 || executor.queryCalls != 1 {
		t.Fatalf(
			"calls = exec:%d query:%d, want 1/1",
			executor.execCalls,
			executor.queryCalls,
		)
	}
}

func TestTransitionThemePackReportsMissingPack(t *testing.T) {
	executor := &themePackTransitionExecutor{
		tag:        pgconn.NewCommandTag("UPDATE 0"),
		queryError: pgx.ErrNoRows,
	}
	_, err := transitionThemePack(
		context.Background(),
		executor,
		"theme_missing",
		domain.ThemePackPublished,
		domain.ThemePackWithdrawn,
		time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC),
	)
	if !errors.Is(err, domain.ErrThemePackNotFound) {
		t.Fatalf("error = %v, want ErrThemePackNotFound", err)
	}
}

func TestTransitionThemePackRequiresExactlyOneAffectedRow(t *testing.T) {
	executor := &themePackTransitionExecutor{
		tag:    pgconn.NewCommandTag("UPDATE 2"),
		status: domain.ThemePackDraft,
	}
	_, err := transitionThemePack(
		context.Background(),
		executor,
		"theme_ocean",
		domain.ThemePackDraft,
		domain.ThemePackPublished,
		time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC),
	)
	if !errors.Is(err, domain.ErrThemeTransitionInvalid) {
		t.Fatalf("error = %v, want ErrThemeTransitionInvalid", err)
	}
}

func TestThemePackTransitionSQLGuardsSourceStatus(t *testing.T) {
	executor := &themePackTransitionExecutor{
		tag:    pgconn.NewCommandTag("UPDATE 0"),
		status: domain.ThemePackDraft,
	}
	_, _ = transitionThemePack(
		context.Background(),
		executor,
		"theme_ocean",
		domain.ThemePackDraft,
		domain.ThemePackPublished,
		time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC),
	)
	if len(executor.statements) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(executor.statements))
	}
	query := strings.Join(strings.Fields(executor.statements[0]), " ")
	if !strings.Contains(query, "WHERE theme_id = $1 AND status = $4") {
		t.Fatalf("transition SQL missing source-state guard: %s", query)
	}
}

type themePackTransitionExecutor struct {
	execCalls  int
	queryCalls int
	tag        pgconn.CommandTag
	status     domain.ThemePackStatus
	queryError error
	statements []string
}

func (executor *themePackTransitionExecutor) Exec(
	_ context.Context,
	sql string,
	_ ...any,
) (pgconn.CommandTag, error) {
	executor.execCalls++
	executor.statements = append(executor.statements, sql)
	return executor.tag, nil
}

func (executor *themePackTransitionExecutor) QueryRow(
	_ context.Context,
	_ string,
	_ ...any,
) pgx.Row {
	executor.queryCalls++
	return themePackStatusRow{
		status: executor.status,
		err:    executor.queryError,
	}
}

type themePackStatusRow struct {
	status domain.ThemePackStatus
	err    error
}

func (row themePackStatusRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	target, ok := dest[0].(*domain.ThemePackStatus)
	if !ok {
		return errors.New("unsupported scan target")
	}
	*target = row.status
	return nil
}
