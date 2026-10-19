package repository

import (
	"context"
	"testing"
)

func TestReadinessFailsClosedWithoutDatabase(t *testing.T) {
	repository := &PostgresRepository{}
	if _, err := repository.Readiness(context.Background()); err == nil {
		t.Fatal("local command readiness must fail without a database")
	}
}
