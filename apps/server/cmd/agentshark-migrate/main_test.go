package main

import (
	"strings"
	"testing"
)

func TestMigrateRejectsMissingDatabaseURLBeforeOpeningPool(t *testing.T) {
	t.Parallel()
	if err := migrate(t.Context(), "  "); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("migrate() error = %v", err)
	}
}
