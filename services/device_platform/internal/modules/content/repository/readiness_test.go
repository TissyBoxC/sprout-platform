package repository

import (
	"context"
	"testing"
)

func TestReadinessFailsClosedWithoutDatabase(t *testing.T) {
	repository := &PostgresRepository{}
	if _, err := repository.Readiness(context.Background()); err == nil {
		t.Fatal("content readiness must fail without a database")
	}
	if _, err := repository.EngagementReadiness(context.Background()); err == nil {
		t.Fatal("engagement readiness must fail without a database")
	}
	if _, err := repository.ThemePackReadiness(context.Background()); err == nil {
		t.Fatal("theme readiness must fail without a database")
	}
}
