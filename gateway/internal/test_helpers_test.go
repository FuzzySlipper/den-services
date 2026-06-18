package gateway

import "time"

func fixedBuiltAt() time.Time {
	return time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
}

func testTimeout() time.Duration {
	return 5 * time.Second
}
