package server

import "testing"

func TestIsReservedSignalName(t *testing.T) {
	reserved := []string{"step-result", "gate-deploy", "gate-reject-deploy"}
	for _, n := range reserved {
		if !isReservedSignalName(n) {
			t.Errorf("signal name %q should be reserved", n)
		}
	}
	allowed := []string{"deploy-ok", "approval", "my-signal", "release"}
	for _, n := range allowed {
		if isReservedSignalName(n) {
			t.Errorf("signal name %q should be allowed", n)
		}
	}
}
