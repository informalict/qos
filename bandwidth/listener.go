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
	// limitReadConn is current read limit config for connection.
	limitReadConn config
	// limitReadConn is current write limit config for connection.
	limitWriteConn config
	// limitReadConn is current global read limit.
	limitReadGlobal config
	// limitReadConn is current global write limit.
	limitWriteGlobal config
	// writeLimiter is a shared global rate limiter across all connections.
	writeLimiter *rate.Limiter
	// readLimiter is a shared global rate limiter across all connections.
	readLimiter *rate.Limiter
}

// NewListener returns bandwidth listener with default infinite global and connection limiters.
// If a given context is canceled then all writes and reads should be interrupted (e.g. SIGTERM was sent).
func NewListener(ctx context.Context, l net.Listener) *listener {
	if l == nil {
		panic("parent listener must be provided")
	}

	unlimited := NewUnlimitedConfig()
	return &listener{
		Listener:         l,
		ctx:              ctx,
		c:                make(chan struct{}),
		limitReadGlobal:  unlimited,
		limitWriteGlobal: unlimited,
		limitReadConn:    unlimited,
		limitWriteConn:   unlimited,
		readLimiter:      unlimited.NewRateLimiter(),
		writeLimiter:     unlimited.NewRateLimiter(),
	}
}

// GetConnCfgs returns write and read connection config.
// It also returns channel, which will be closed when configuration is changed.
func (bl *listener) GetConnCfgs() (<-chan struct{}, config, config) {
	bl.mutex.RLock()
	defer bl.mutex.RUnlock()

	return bl.c, bl.limitWriteConn, bl.limitReadConn
}

// GetGlobalLimits gets global limits for writing and reading from a connection.
func (bl *listener) GetGlobalLimits() (config, config) {
	bl.mutex.RLock()
	defer bl.mutex.RUnlock()

	return bl.limitWriteGlobal, bl.limitReadGlobal
}

// GetConnLimits gets limits per connection for writing and reading.
func (bl *listener) GetConnLimits() (config, config) {
	// Here mutex blocks new connections and writing, and reading.
	// It happens once in a lifetime, and it is quick non-blocking operation it is allowed.
	bl.mutex.RLock()
	defer bl.mutex.RUnlock()

	return bl.limitWriteConn, bl.limitReadConn
}

// SetGlobalLimits sets global limits for writing and reading.
func (bl *listener) SetGlobalLimits(writeGlobal, readGlobal config) {
	bl.mutex.Lock()
	defer bl.mutex.Unlock()

	bl.limitWriteGlobal = writeGlobal
	bl.writeLimiter.SetLimit(writeGlobal.limit)
	bl.writeLimiter.SetBurst(writeGlobal.burst)
	bl.limitReadGlobal = readGlobal
	bl.readLimiter.SetLimit(readGlobal.limit)
	bl.readLimiter.SetBurst(readGlobal.burst)
}

// SetConnLimits sets limit per connection for writing and reading.
func (bl *listener) SetConnLimits(writeConn, readConn config) {
	// Here mutex blocks new connections and writing, and reading.
	// It happens once in a lifetime, and it is quick non-blocking operation it is allowed.
	bl.mutex.Lock()
	defer bl.mutex.Unlock()

	if bl.limitWriteConn.IsTheSame(writeConn) && bl.limitReadConn.IsTheSame(readConn) {
		// Nothing changes.
		return
	}

	// Inform all existing connections about new configuration by closing channel.
	// Create a new channel which will be closed when config changes next time, so
	// connection will be informed once again.
	close(bl.c)
	bl.c = make(chan struct{})

	bl.limitWriteConn = writeConn
	bl.limitReadConn = readConn
}

// WaitWriteN waits until global limiter allows for writing n bytes.
func (bl *listener) WaitWriteN(ctx context.Context, n int) error {
	return bl.writeLimiter.WaitN(ctx, n)
}

// WaitReadN waits until global limiter allows for reading n bytes.
func (bl *listener) WaitReadN(ctx context.Context, n int) error {
	return bl.readLimiter.WaitN(ctx, n)
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
		Conn:             conn,
		ctx:              bl.ctx,
		connWriteLimiter: bl.limitWriteConn.NewRateLimiter(),
		connReadLimiter:  bl.limitReadConn.NewRateLimiter(),
		controller:       bl,
		// pass read only channel, which will be closed when config is changed.
		c: bl.c,
	}, nil
}
