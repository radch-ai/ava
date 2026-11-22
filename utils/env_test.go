package utils

import "testing"

// A malformed limit is an error, never a silent default: the caller must
// answer it (a defaulted bound would weaken the policy).
func TestGetEnvInt(t *testing.T) {
	t.Setenv("AVA_TEST_LIMIT", "")
	if got, err := GetEnvInt("AVA_TEST_LIMIT", 30); err != nil || got != 30 {
		t.Fatalf("unset = %d, %v; want 30, nil", got, err)
	}

	t.Setenv("AVA_TEST_LIMIT", "45")
	if got, err := GetEnvInt("AVA_TEST_LIMIT", 30); err != nil || got != 45 {
		t.Fatalf("set = %d, %v; want 45, nil", got, err)
	}

	t.Setenv("AVA_TEST_LIMIT", "many")
	if _, err := GetEnvInt("AVA_TEST_LIMIT", 30); err == nil {
		t.Fatal("malformed value accepted")
	}
}
