package core

import "testing"

func TestRegistrationURL(t *testing.T) {
	t.Setenv(EnvDomain, "https://mirror.example")

	// The explicit mirror is used as the registration page directly.
	if got := RegistrationURL("https://z-lib.gd"); got != "https://z-lib.gd" {
		t.Errorf("RegistrationURL(explicit) = %q, want %q", got, "https://z-lib.gd")
	}
	// An empty domain falls back to the configured default mirror.
	if got := RegistrationURL(""); got != "https://mirror.example" {
		t.Errorf("RegistrationURL(empty) = %q, want %q", got, "https://mirror.example")
	}
}
