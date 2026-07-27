package user_profiles

import "testing"

func TestValidateProfileJSONRejectsInvalidJSON(t *testing.T) {
	if err := validateProfileJSON(`{"preferences":{"categories":["手机"]}}`); err != nil {
		t.Fatalf("validateProfileJSON() error = %v", err)
	}
	if err := validateProfileJSON(`{"preferences":`); err == nil {
		t.Fatal("validateProfileJSON() error = nil, want invalid JSON error")
	}
}
