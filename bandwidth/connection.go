package bandwidth

import (
	"context"
	"net"
	"sync"

	"golang.org/x/time/rate"
)

// globalLimitController is an internal interface which allows for connection
// to get access to listener's data.
// So the whole object listener does not have to be provided to connection.
type globalLimitController interface {
	// GetConnCfgs returns current write and read connection config.
	// It returns also a channel which will be closed when config is changed again.
	GetConnCfgs() (<-chan struct{}, config, config)
	// WaitWriteN waits until global limiter allows for writing n bytes.
	WaitWriteN(ctx context.Context, n int) (err error)
	// WaitReadN waits until global limiter allows for reading n bytes.
	WaitReadN(ctx context.Context, n int) (err error)
}

type connection struct {
	net.Conn
	ctx              context.Context
	mutex            sync.Mutex
	connWriteLimiter *rate.Limiter
	connReadLimiter  *rate.Limiter
	controller       globalLimitController
	// c is closed when configuration is changed, so current connection can read new config immediately.
	c <-chan struct{}
}

// Write writes bytes into connection with respect to global and connection limiter.
func (bc *connection) Write(b []byte) (int, error) {
	select {
	case <-bc.c:
		// This channel can be only closed, so there is no need to check if something was populated into it.
		bc.setLimiter(true)
	default:
		// Configuration per connection has not been changed.
	}

	// First of all wait for connection limiter permission.
	// If it is not fulfilled then global limiter should not be blocked.
	if err := bc.connWriteLimiter.WaitN(bc.ctx, len(b)); err != nil {
		return 0, err
	}

	// Now connection is ready to write bytes, so global limiter must be checked.
	if err := bc.controller.WaitWriteN(bc.ctx, len(b)); err != nil {
		return 0, err
	}

	return bc.Conn.Write(b)
}

// Read reads bytes from a connection with respect to global and connection limiter.
func (bc *connection) Read(b []byte) (int, error) {
	select {
	case <-bc.c:
		// This channel can be only closed, so there is no need to check if something was populated into it.
		bc.setLimiter(false)
	default:
		// Configuration per connection has not been changed.
	}

	// First of all wait for connection limiter permission.
	// If it is not fulfilled then global limiter should not be blocked.
	if err := bc.connReadLimiter.WaitN(bc.ctx, len(b)); err != nil {
		return 0, err
	}

	// Now connection is ready to read bytes, so global limiter must be checked.
	if err := bc.controller.WaitReadN(bc.ctx, len(b)); err != nil {
		return 0, err
	}

	return bc.Conn.Read(b)
}

func (bc *connection) setLimiter(isWriter bool) {
	c, newWriteCfg, newReadCfg := bc.controller.GetConnCfgs()

	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	bc.c = c
	if isWriter {
		bc.connWriteLimiter.SetLimit(newWriteCfg.limit)
		bc.connWriteLimiter.SetBurst(newWriteCfg.burst)
	} else {
		bc.connReadLimiter.SetLimit(newReadCfg.limit)
		bc.connReadLimiter.SetBurst(newReadCfg.burst)
	}
}
