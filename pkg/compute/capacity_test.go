package compute

import "testing"

func TestCapacityType_Interruptible(t *testing.T) {
	if !CapacitySpot.Interruptible() {
		t.Error("spot must be interruptible")
	}
	if CapacityOnDemand.Interruptible() {
		t.Error("on_demand (stable) must not be interruptible")
	}
	if CapacityAny.Interruptible() {
		t.Error("any is not itself interruptible")
	}
}

func TestDegradeCapacity(t *testing.T) {
	both := []CapacityType{CapacityOnDemand, CapacitySpot}
	stableOnly := []CapacityType{CapacityOnDemand}
	spotOnly := []CapacityType{CapacitySpot}

	tests := []struct {
		name        string
		requested   CapacityType
		supported   []CapacityType
		want        CapacityType
		wantChanged bool
	}{
		{"any is untouched", CapacityAny, stableOnly, CapacityAny, false},
		{"empty is untouched", "", stableOnly, "", false},
		{"supported spot stays spot", CapacitySpot, both, CapacitySpot, false},
		{"supported stable stays stable", CapacityOnDemand, both, CapacityOnDemand, false},
		{
			name:      "interruptible on a stable-only provider upgrades to stable",
			requested: CapacitySpot, supported: stableOnly,
			want: CapacityOnDemand, wantChanged: true,
		},
		{
			// Never silently hand back interruptible for a stable request — the
			// pool asked for reliability. Leave it; Quote returns no offers.
			name:      "stable on a spot-only provider is NOT downgraded",
			requested: CapacityOnDemand, supported: spotOnly,
			want: CapacityOnDemand, wantChanged: false,
		},
		{
			name:      "unsupported class with no fallback is left unchanged",
			requested: CapacitySpot, supported: nil,
			want: CapacitySpot, wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := DegradeCapacity(tt.requested, tt.supported)
			if got != tt.want || changed != tt.wantChanged {
				t.Errorf("DegradeCapacity(%q, %v) = (%q, %v), want (%q, %v)",
					tt.requested, tt.supported, got, changed, tt.want, tt.wantChanged)
			}
		})
	}
}
