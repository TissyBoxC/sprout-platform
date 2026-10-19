package service

import (
	"context"
	"testing"
)

func TestContentCatalogReaderFailsClosedWithoutRepository(t *testing.T) {
	reader := ContentCatalogReader{}
	if _, err := reader.ListCandidates(context.Background(), "", ""); err == nil {
		t.Fatal("missing content repository must return an error")
	}
}

func TestContentSignalReaderFailsClosedWithoutDependencies(t *testing.T) {
	reader := ContentSignalReader{}
	if _, err := reader.FavoritePackageIDs(context.Background(), "family-1"); err == nil {
		t.Fatal("missing content repository must return an error")
	}
	if _, err := reader.RecentPackageIDs(context.Background(), "family-1", 8); err == nil {
		t.Fatal("missing playback device reader must return an error")
	}
}

func TestBindingDeviceReaderFailsClosedWithoutService(t *testing.T) {
	reader := BindingDeviceReader{}
	if _, err := reader.FamilyDeviceIDs(context.Background(), "family-1"); err == nil {
		t.Fatal("missing device binding service must return an error")
	}
}
