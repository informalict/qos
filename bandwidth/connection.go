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
	// GetConnCfg returns current connection config.
	// It returns also a channel which will be closed when config is changed again.
	GetConnCfg() (<-chan struct{}, config)
	// WaitN waits until global limiter allows for operating on n bytes.
	WaitN(ctx context.Context, n int) (err error)
}

type connection struct {
	net.Conn
	ctx        context.Context
	mutex      sync.Mutex
	limiter    *rate.Limiter
	controller globalLimitController
	// c is closed when configuration is changed, so current connection can read new config immediately.
	c <-chan struct{}
}

// Write writes bytes into connection with respect to global and connection limiter.
func (bc *connection) Write(b []byte) (int, error) {
	if err := bc.waitN(b); err != nil {
		return 0, err
	}

	return bc.Conn.Write(b)
}

// Read reads bytes from a connection with respect to global and connection limiter.
func (bc *connection) Read(b []byte) (int, error) {
	if err := bc.waitN(b); err != nil {
		return 0, err
	}

	return bc.Conn.Read(b)
}

func (bc *connection) waitN(b []byte) error {
	select {
	case <-bc.c:
		// This channel can be only closed, so there is no need to check if something was populated into it.
		bc.setLimiter()
	default:
		// Configuration per connection has not been changed.
	}

	// First of all wait for connection limiter permission.
	// If it is not fulfilled then global limiter should not be blocked.
	if err := bc.limiter.WaitN(bc.ctx, len(b)); err != nil {
		return err
	}

	// Now connection is ready to read bytes, so global limiter must be checked.
	if err := bc.controller.WaitN(bc.ctx, len(b)); err != nil {
		return err
	}

	return nil
}

func (bc *connection) setLimiter() {
	c, newCfg := bc.controller.GetConnCfg()

	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	bc.c = c
	if newCfg.limit == bc.limiter.Limit() && newCfg.burst == bc.limiter.Burst() {
		// It may happen that Read and Write compete with each other,
		// so maybe one of them already changed it.
		return
	}

	bc.limiter.SetLimit(newCfg.limit)
	bc.limiter.SetBurst(newCfg.burst)
}
