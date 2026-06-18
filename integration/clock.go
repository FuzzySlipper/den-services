package integration

import "time"

type FixedClock struct {
	now time.Time
}

func NewFixedClock(now time.Time) *FixedClock {
	return &FixedClock{now: now.UTC()}
}

func (c *FixedClock) Now() time.Time {
	return c.now
}

func (c *FixedClock) Advance(duration time.Duration) {
	c.now = c.now.Add(duration)
}
