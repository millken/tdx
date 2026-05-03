package tdx

import "time"

const (
	defaultTimeout = 8 * time.Second
)

type config struct {
	timeout time.Duration
	sp      bool
	ex      bool
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

// WithSP enables SP mode login (mac_quotation protocol) after bootstrap.
func WithSP() Option {
	return func(c *config) {
		c.sp = true
		c.ex = false
	}
}

// WithEx enables extension quote mode (7727 protocol).
func WithEx() Option {
	return func(c *config) {
		c.ex = true
		c.sp = false
	}
}
