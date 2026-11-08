package cgroups_test

import (
	"runtime"
	"strings"
	"testing"
	"vpsbot/internal/cgroups"
)

func TestCgroups_SetMode(t *testing.T) {
	mgr := cgroups.New("x-ui.service", "omniroute.service")

	// On non-Linux (e.g. Windows dev machine), it should return a graceful warning rather than panic
	out, err := mgr.SetMode(cgroups.ModeVPN)
	if runtime.GOOS != "linux" {
		if err != nil {
			t.Fatalf("expected nil error on non-linux warning, got: %v", err)
		}
		if !strings.Contains(out, "Linux") {
			t.Errorf("expected Linux warning, got: %s", out)
		}
	}

	// Test invalid mode
	_, err = mgr.SetMode(cgroups.Mode("invalid_mode"))
	if err == nil {
		t.Error("expected error for invalid mode, got nil")
	}
}

