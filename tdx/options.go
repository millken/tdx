package tdx

import "time"

const (
	defaultTimeout = 8 * time.Second
)

type config struct {
	timeout time.Duration
	debug   bool
}

func defaultConfig() config {
	return config{
		timeout: defaultTimeout,
	}
}

// Option configures a Client.
type Option func(*config)

// WithTimeout sets the dial and bootstrap timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithDebug enables debug logging to stderr.
func WithDebug() Option {
	return func(c *config) {
		c.debug = true
	}
}
