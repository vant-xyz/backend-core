package db

import "testing"

func TestParseMigrationFilename_ValidFile(t *testing.T) {
	version, label, err := parseMigrationFilename("0001_create_users.sql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != 1 {
		t.Errorf("version = %d, want 1", version)
	}
	if label != "create users" {
		t.Errorf("label = %q, want %q", label, "create users")
	}
}

func TestParseMigrationFilename_UnderscoresInLabelBecomeSpaces(t *testing.T) {
	_, label, err := parseMigrationFilename("0009_create_settlement_payouts.sql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "create settlement payouts"
	if label != want {
		t.Errorf("label = %q, want %q", label, want)
	}
}

func TestParseMigrationFilename_LargeVersionNumber(t *testing.T) {
	version, _, err := parseMigrationFilename("0042_add_indexes.sql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != 42 {
		t.Errorf("version = %d, want 42", version)
	}
}

func TestParseMigrationFilename_MissingUnderscore_ReturnsError(t *testing.T) {
	_, _, err := parseMigrationFilename("badfilename.sql")
	if err == nil {
		t.Error("missing underscore separator should return error")
	}
}

func TestParseMigrationFilename_NonNumericVersion_ReturnsError(t *testing.T) {
	_, _, err := parseMigrationFilename("abcd_create_users.sql")
	if err == nil {
		t.Error("non-numeric version prefix should return error")
	}
}

func TestParseMigrationFilename_StripsSqlExtensionBeforeParsing(t *testing.T) {
	version, label, err := parseMigrationFilename("0003_create_balances.sql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != 3 {
		t.Errorf("version = %d, want 3", version)
	}
	if label != "create balances" {
		t.Errorf("label = %q, want %q", label, "create balances")
	}
}
