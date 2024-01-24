package bandwidth

import (
	"context"
	"net"
	"sync"

	"golang.org/x/time/rate"
)

type listener struct {
	// Listener is an original listener which wrapped by bandwidth listener.
	net.Listener
	// ctx is a context which can be canceled, so all Write functions will be canceled immediately.
	ctx context.Context
	// c is closed when configuration for connections is changed, so all existing connections can read new config.
	c     chan struct{}
	mutex sync.RWMutex
	// limitCfgConn is current limit config for a connection.
	limitCfgConn config
	// limitCfgGlobal is a current global limit.
	limitCfgGlobal config
	// sharedLimiter is a shared global rate limiter across all connections.
	sharedLimiter *rate.Limiter
}

// NewListener returns bandwidth listener with default infinite global and connection limiters.
// If a given context is canceled then all writes and reads should be interrupted (e.g. SIGTERM was sent).
func NewListener(ctx context.Context, l net.Listener) *listener {
	if l == nil {
		panic("parent listener must be provided")
	}

	unlimited := NewUnlimitedConfig()
	return &listener{
		Listener:       l,
		ctx:            ctx,
		c:              make(chan struct{}),
		limitCfgConn:   unlimited,
		limitCfgGlobal: unlimited,
		sharedLimiter:  unlimited.NewRateLimiter(),
	}
}

// GetConnCfg returns connection config.
// It also returns channel, which will be closed when configuration is changed.
func (bl *listener) GetConnCfg() (<-chan struct{}, config) {
	bl.mutex.RLock()
	defer bl.mutex.RUnlock()

	return bl.c, bl.limitCfgConn
}

// GetLimits returns global and connection limits.
func (bl *listener) GetLimits() (config, config) {
	bl.mutex.RLock()
	defer bl.mutex.RUnlock()

	return bl.limitCfgGlobal, bl.limitCfgConn
}

// SetLimits sets global and connection limits for writing and reading.
func (bl *listener) SetLimits(globalCfg, connCfg config) {
	bl.mutex.Lock()
	defer bl.mutex.Unlock()

	bl.limitCfgGlobal = globalCfg
	bl.sharedLimiter.SetLimit(globalCfg.limit)
	bl.sharedLimiter.SetBurst(globalCfg.burst)

	if bl.limitCfgConn.IsTheSame(connCfg) {
		// Nothing changes for connections.
		return
	}

	// Inform all existing connections about new configuration by closing channel.
	// Create a new channel which will be closed when config changes next time, so
	// connection will be informed once again.
	close(bl.c)
	bl.c = make(chan struct{})

	bl.limitCfgConn = connCfg
}

// WaitN waits until global limiter allows for operating on n bytes.
func (bl *listener) WaitN(ctx context.Context, n int) error {
	return bl.sharedLimiter.WaitN(ctx, n)
}

// Accept returns accepted bandwidth connection.
func (bl *listener) Accept() (net.Conn, error) {
	conn, err := bl.Listener.Accept()
	if err != nil {
		return conn, err
	}

	// Here mutex does not block connection's writers.
	bl.mutex.RLock()
	defer bl.mutex.RUnlock()

	return &connection{
		Conn:       conn,
		ctx:        bl.ctx,
		limiter:    bl.limitCfgConn.NewRateLimiter(),
		controller: bl,
		// pass read only channel, which will be closed when config is changed.
		c: bl.c,
	}, nil
}
