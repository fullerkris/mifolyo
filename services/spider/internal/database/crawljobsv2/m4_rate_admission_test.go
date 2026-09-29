package crawljobsv2

import "testing"

// Fresh, identical prefixes independently exercise D-1 rejection and D/D+1
// admission. These facade clock cuts are not live Redis elapsed-time evidence.
func TestM4PositiveIntervalOffline(t *testing.T) {
	m4RequestLifecycleProfiles(t, true)
}
