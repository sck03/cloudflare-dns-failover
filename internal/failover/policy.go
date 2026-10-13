// Package failover decides when to switch without performing network or database I/O.
package failover

type Policy struct {
	Primary, Backup                     string
	FailureThreshold, RecoveryThreshold int
}

type State struct {
	Current, Preferred  string
	Failures, Successes int
}

// Decide advances consecutive observation counts. Current is deliberately left
// unchanged: the caller may commit a target only after the DNS provider confirms it.
func Decide(p Policy, s State, primaryUp, backupUp bool) (State, string) {
	if s.Preferred != p.Primary && s.Preferred != p.Backup {
		s.Preferred = p.Primary
	}
	healthy := func(address string) bool {
		return address == p.Primary && primaryUp || address == p.Backup && backupUp
	}
	s.Failures = streak(s.Failures, !healthy(s.Current), p.FailureThreshold)
	s.Successes = streak(s.Successes, healthy(s.Preferred), p.RecoveryThreshold)
	if s.Current == s.Preferred {
		s.Successes = 0
		if s.Failures < max(p.FailureThreshold, 1) {
			return s, ""
		}
		other := p.Backup
		if s.Current == p.Backup {
			other = p.Primary
		}
		if healthy(other) {
			return s, other
		}
	} else if healthy(s.Preferred) &&
		(s.Successes >= max(p.RecoveryThreshold, 1) || s.Failures >= max(p.FailureThreshold, 1)) {
		return s, s.Preferred
	}
	return s, ""
}

func streak(previous int, matches bool, threshold int) int {
	if !matches {
		return 0
	}
	return min(previous+1, max(threshold, 1))
}
