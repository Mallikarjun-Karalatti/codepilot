package main

import (
	"testing"
)

func TestIndexConfigFingerprintIsStableAndSensitive(t *testing.T) {
	base := DefaultIndexConfig()
	base.EmbeddingDimension = 2
	base.EmbeddingModel = "test"

	first, err := base.Fingerprint()
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	reordered := base
	reordered.ExcludedDirectories = []string{"vendor", ".git"}
	second, err := reordered.Fingerprint()
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	if first != second {
		t.Fatalf("fingerprint changed when exclusion order changed: %s vs %s", first, second)
	}

	changed := base
	changed.IncludeTests = true
	third, err := changed.Fingerprint()
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	if first == third {
		t.Fatal("fingerprint did not change when IncludeTests changed")
	}
}

func TestCanonicalRepositoryIDUsesAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	id1, err := CanonicalRepositoryID(dir)
	if err != nil {
		t.Fatalf("CanonicalRepositoryID() error = %v", err)
	}
	id2, err := CanonicalRepositoryID(dir + "/")
	if err != nil {
		t.Fatalf("CanonicalRepositoryID() error = %v", err)
	}
	if id1 != id2 {
		t.Fatalf("ids differ for equivalent paths: %s vs %s", id1, id2)
	}
	if id1 == "" {
		t.Fatal("repository id is empty")
	}
}
