package bandwidth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestNewConfig(t *testing.T) {

	tests := map[string]struct {
		limit rate.Limit
		burst []int
		want  config
	}{
		"infinite config case 1": {
			limit: -1,
			burst: nil,
			want: config{
				limit: rate.Inf,
			},
		},
		"infinite config case 2": {
			limit: 0,
			burst: []int{1, 2, 3},
			want: config{
				limit: rate.Inf,
			},
		},
		"infinite config case 3": {
			limit: rate.Inf,
			burst: []int{20},
			want: config{
				limit: rate.Inf,
			},
		},
		"adjust burst": {
			limit: 20,
			burst: nil,
			want: config{
				limit: 20,
				burst: 20,
			},
		},
		"valid config": {
			limit: 20,
			burst: []int{19},
			want: config{
				limit: 20,
				burst: 19,
			},
		},
	}
	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			c := NewConfig(test.limit, test.burst...)
			assert.Equal(t, test.want, c)
			r := c.NewRateLimiter()
			assert.Equal(t, c.limit, r.Limit())
			assert.Equal(t, c.burst, r.Burst())
		})
	}
}

func TestUnlimitedConfig(t *testing.T) {
	c := NewUnlimitedConfig()
	assert.Equal(t, rate.Inf, c.limit)
	assert.Equal(t, 0, c.burst)
}

func TestIsTheSame(t *testing.T) {
	c1, c2 := NewConfig(10), NewConfig(10)
	assert.Equal(t, true, c1.IsTheSame(c2))

	c1, c2 = NewConfig(10, 20), NewConfig(10, 21)
	assert.Equal(t, false, c1.IsTheSame(c2))

	c1, c2 = NewConfig(11), NewConfig(11)
	assert.Equal(t, true, c1.IsTheSame(c2))

	c1, c2 = NewConfig(11), NewConfig(10)
	assert.Equal(t, false, c1.IsTheSame(c2))
}
