package failover

import "testing"

func TestDecide(t *testing.T) {
	p := Policy{Primary: "primary", Backup: "backup", FailureThreshold: 3, RecoveryThreshold: 2}
	for _, tc := range []struct {
		name       string
		state      State
		primary    bool
		backup     bool
		wantTarget string
		failures   int
		successes  int
	}{
		{"failure below threshold", State{Current: "primary", Failures: 1}, false, true, "", 2, 0},
		{"fail over to healthy backup", State{Current: "primary", Failures: 2}, false, true, "backup", 3, 0},
		{"both down keep current", State{Current: "primary", Failures: 3}, false, false, "", 3, 0},
		{"success breaks failure streak", State{Current: "primary", Failures: 2}, true, true, "", 0, 0},
		{"recovery below threshold", State{Current: "backup"}, true, true, "", 0, 1},
		{"recover after consecutive successes", State{Current: "backup", Successes: 1}, true, true, "primary", 0, 2},
		{"failure breaks recovery streak", State{Current: "backup", Successes: 1}, false, true, "", 0, 0},
		{"scheduled backup stays active", State{Current: "backup", Preferred: "backup"}, true, true, "", 0, 0},
		{"scheduled backup fails", State{Current: "backup", Preferred: "backup", Failures: 2}, true, false, "primary", 3, 0},
		{"recover scheduled preference", State{Current: "primary", Preferred: "backup", Successes: 1}, true, true, "backup", 0, 2},
		{"saturated retry after API failure", State{Current: "primary", Failures: 3}, false, true, "backup", 3, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, target := Decide(p, tc.state, tc.primary, tc.backup)
			if target != tc.wantTarget || state.Failures != tc.failures || state.Successes != tc.successes {
				t.Fatalf("got state %+v, target %q; want failures=%d successes=%d target=%q", state, target, tc.failures, tc.successes, tc.wantTarget)
			}
			if state.Current != tc.state.Current {
				t.Fatal("decision changed Current before a confirmed DNS update")
			}
		})
	}
}

func BenchmarkDecide(b *testing.B) {
	p := Policy{Primary: "primary", Backup: "backup", FailureThreshold: 3, RecoveryThreshold: 2}
	s := State{Current: "primary"}
	for b.Loop() {
		Decide(p, s, false, true)
	}
}
