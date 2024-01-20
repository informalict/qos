package bandwidth

import (
	"golang.org/x/time/rate"
)

type config struct {
	limit rate.Limit
	burst int
}

// NewConfig creates new config limiter for given limit and optional burst.
// When burst is not provided, or it is <=0 then it be the same as limit.
func NewConfig(limit rate.Limit, burst ...int) config {
	if limit <= 0 || limit == rate.Inf {
		return config{limit: rate.Inf, burst: 0}
	}

	c := config{
		limit: limit,
	}
	if len(burst) > 0 && burst[0] > 0 {
		c.burst = burst[0]
	} else {
		// Rate limiter will not work with burst 0 and limit > 0, unless limit is Inf.
		c.burst = int(limit)
	}

	return c
}

// NewUnlimitedConfig returns new unlimited config.
func NewUnlimitedConfig() config {
	return NewConfig(rate.Inf)
}

// NewRateLimiter returns new rate limiter.
func (c config) NewRateLimiter() *rate.Limiter {
	// Validation is not required here, because it was done when object was created.
	return rate.NewLimiter(c.limit, c.burst)
}

// IsTheSame returns true if two configs are the same.
func (c config) IsTheSame(other config) bool {
	return c.limit == other.limit && c.burst == other.burst
}
