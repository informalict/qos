package bandwidth

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

const (
	minThreshold = 0.95
	maxThreshold = 1.05
)

type OperationFunc func() int

// TestLongDuration runs long tests and check if rate is expected.
func TestLongDuration(tOuter *testing.T) {
	if testing.Short() {
		tOuter.Skip("testing argument -short is turned on")
	}

	// Setting for all below sub-tests.
	expectedDuration := time.Second * 30
	var rateLimit rate.Limit = 1000

	testName := fmt.Sprintf("check connection write rate after %s", expectedDuration.String())
	tOuter.Run(testName, func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), expectedDuration)
		defer cancel()

		bl := NewListener(ctx, mockListener{})
		_, cr := bl.GetConnLimits()
		bl.SetConnLimits(NewConfig(rateLimit), cr)
		conn := acceptT(t, bl)
		b := newSlice(int(rateLimit))

		var op OperationFunc = func() int {
			var counter, n int
			var err error

			for err == nil {
				n, err = conn.Write(b)
				counter += n
			}

			return counter
		}

		expectedBytes := int(expectedDuration.Seconds()) * int(rateLimit)
		checkRate(t, expectedBytes, expectedDuration, op)
	})

	testName = fmt.Sprintf("check connection read rate after %s", expectedDuration.String())
	tOuter.Run(testName, func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), expectedDuration)
		defer cancel()

		bl := NewListener(ctx, mockListener{})
		cw, _ := bl.GetConnLimits()
		bl.SetConnLimits(cw, NewConfig(rateLimit))
		conn := acceptT(t, bl)
		b := newSlice(int(rateLimit))

		var op OperationFunc = func() int {
			var counter, n int
			var err error

			for err == nil {
				n, err = conn.Read(b)
				counter += n
			}

			return counter
		}

		expectedBytes := int(expectedDuration.Seconds()) * int(rateLimit)
		checkRate(t, expectedBytes, expectedDuration, op)
	})

	testName = fmt.Sprintf("check global write rate after %s", expectedDuration.String())
	tOuter.Run(testName, func(t *testing.T) {
		t.Parallel()

		// TODO hack with one second, because setGlobalLimits sleeps one second.
		ctx, cancel := context.WithTimeout(context.Background(), expectedDuration+time.Second)
		defer cancel()

		bl := NewListener(ctx, mockListener{})
		_, gr := bl.GetGlobalLimits()
		setGlobalLimits(bl, NewConfig(rateLimit), gr)
		conn := acceptT(t, bl)
		b := newSlice(int(rateLimit))

		var op OperationFunc = func() int {
			var counter, n int
			var err error

			for err == nil {
				n, err = conn.Write(b)
				counter += n
			}

			return counter
		}

		expectedBytes := int(expectedDuration.Seconds()) * int(rateLimit)
		checkRate(t, expectedBytes, expectedDuration, op)
	})

	testName = fmt.Sprintf("check global read rate after %s", expectedDuration.String())
	tOuter.Run(testName, func(t *testing.T) {
		t.Parallel()

		// TODO hack with one second, because setGlobalLimits sleeps one second.
		ctx, cancel := context.WithTimeout(context.Background(), expectedDuration+time.Second)
		defer cancel()

		bl := NewListener(ctx, mockListener{})
		gw, _ := bl.GetGlobalLimits()
		setGlobalLimits(bl, gw, NewConfig(rateLimit))
		conn := acceptT(t, bl)
		b := newSlice(int(rateLimit))

		var op OperationFunc = func() int {
			var counter, n int
			var err error

			for err == nil {
				n, err = conn.Read(b)
				counter += n
			}

			return counter
		}

		expectedBytes := int(expectedDuration.Seconds()) * int(rateLimit)
		checkRate(t, expectedBytes, expectedDuration, op)
	})
}

// TestOneConnectionManyClients test one connection which can be used simultaneously.
func TestOneConnectionManyClients(tOuter *testing.T) {
	tOuter.Run("1 connection with two simultaneous writes", func(t *testing.T) {
		t.Parallel()
		var limit rate.Limit = 10
		howManyBytes := 40

		bl := NewListener(context.Background(), mockListener{})
		_, cr := bl.GetConnLimits()
		bl.SetConnLimits(NewConfig(limit), cr)
		conn := acceptT(t, bl)
		b := newSlice(int(limit))

		var op OperationFunc = func() int {
			var counter1, counter2 int
			wg := sync.WaitGroup{}
			howManyClients := 2
			wg.Add(howManyClients)
			go func() {
				defer wg.Done()
				for counter1 != howManyBytes/howManyClients {
					counter1 += writeT(t, conn, b)
				}
			}()
			go func() {
				defer wg.Done()
				for counter2 != howManyBytes/howManyClients {
					counter2 += writeT(t, conn, b)
				}
			}()

			wg.Wait()

			return counter1 + counter2
		}

		checkRate(t, howManyBytes, time.Second*4, op)
	})
	tOuter.Run("1 connection with two simultaneous reads", func(t *testing.T) {
		t.Parallel()
		var limit rate.Limit = 10
		howManyBytes := 40

		bl := NewListener(context.Background(), mockListener{})
		cw, _ := bl.GetConnLimits()
		bl.SetConnLimits(cw, NewConfig(limit))
		conn := acceptT(t, bl)
		b := newSlice(int(limit))

		var op OperationFunc = func() int {
			var counter1, counter2 int
			wg := sync.WaitGroup{}
			howManyClients := 2
			wg.Add(howManyClients)
			go func() {
				defer wg.Done()
				for counter1 != howManyBytes/howManyClients {
					counter1 += readT(t, conn, b)
				}
			}()
			go func() {
				defer wg.Done()
				for counter2 != howManyBytes/howManyClients {
					counter2 += readT(t, conn, b)
				}
			}()

			wg.Wait()

			return counter1 + counter2
		}

		checkRate(t, howManyBytes, time.Second*4, op)
	})
}

// TestTwoConnections tests 2 connection which will block each other, because of global limit.
func TestTwoConnections(tOuter *testing.T) {
	tOuter.Run("2 connections compete for write global rate", func(t *testing.T) {
		t.Parallel()
		var rateGlobal rate.Limit = 10
		howManyBytes1, howManyBytes2 := 50, 40

		bl := NewListener(context.Background(), mockListener{})
		_, gr := bl.GetGlobalLimits()
		setGlobalLimits(bl, NewConfig(rateGlobal), gr)

		conn1 := acceptT(t, bl)
		conn2 := acceptT(t, bl)

		b := newSlice(int(rateGlobal))
		var op OperationFunc = func() int {
			var counter1, counter2 int
			wg := sync.WaitGroup{}
			wg.Add(2)
			go func() {
				defer wg.Done()
				for counter1 != howManyBytes1 {
					counter1 += writeT(t, conn1, b)
				}
			}()
			go func() {
				defer wg.Done()
				for counter2 != howManyBytes2 {
					counter2 += writeT(t, conn2, b)
				}
			}()

			wg.Wait()

			return counter1 + counter2
		}

		checkRate(t, howManyBytes1+howManyBytes2, time.Second*9, op)
	})

	tOuter.Run("2 connections compete for read global rate", func(t *testing.T) {
		t.Parallel()
		var rateGlobal rate.Limit = 10
		howManyBytes1, howManyBytes2 := 50, 40

		bl := NewListener(context.Background(), mockListener{})
		gw, _ := bl.GetGlobalLimits()
		setGlobalLimits(bl, gw, NewConfig(rateGlobal))
		conn1 := acceptT(t, bl)
		conn2 := acceptT(t, bl)

		b := newSlice(int(rateGlobal))
		var op OperationFunc = func() int {
			var counter1, counter2 int
			wg := sync.WaitGroup{}
			wg.Add(2)
			go func() {
				defer wg.Done()
				for counter1 != howManyBytes1 {
					counter1 += readT(t, conn1, b)
				}
			}()
			go func() {
				defer wg.Done()
				for counter2 != howManyBytes2 {
					counter2 += readT(t, conn2, b)
				}
			}()

			wg.Wait()

			return counter1 + counter2
		}

		checkRate(t, howManyBytes1+howManyBytes2, time.Second*9, op)
	})

	tOuter.Run("2 connections don't compete for write connection rate", func(t *testing.T) {
		t.Parallel()
		var rateConn rate.Limit = 10
		howManyBytes1, howManyBytes2 := 50, 40

		bl := NewListener(context.Background(), mockListener{})
		_, cr := bl.GetConnLimits()
		bl.SetConnLimits(NewConfig(rateConn), cr)
		conn1 := acceptT(t, bl)
		conn2 := acceptT(t, bl)

		b := newSlice(int(rateConn))
		var op OperationFunc = func() int {
			var counter1, counter2 int
			wg := sync.WaitGroup{}
			wg.Add(2)
			go func() {
				defer wg.Done()
				for counter1 != howManyBytes1 {
					counter1 += writeT(t, conn1, b)
				}
			}()
			go func() {
				defer wg.Done()
				for counter2 != howManyBytes2 {
					counter2 += writeT(t, conn2, b)
				}
			}()

			wg.Wait()

			return counter1 + counter2
		}

		checkRate(t, howManyBytes1+howManyBytes2, time.Second*5, op)
	})

	tOuter.Run("2 connections don't compete for read connection rate", func(t *testing.T) {
		t.Parallel()
		var rateConn rate.Limit = 10
		howManyBytes1, howManyBytes2 := 50, 40

		bl := NewListener(context.Background(), mockListener{})
		cw, _ := bl.GetConnLimits()
		bl.SetConnLimits(cw, NewConfig(rateConn))
		conn1 := acceptT(t, bl)
		conn2 := acceptT(t, bl)

		b := newSlice(int(rateConn))
		var op OperationFunc = func() int {
			var counter1, counter2 int
			wg := sync.WaitGroup{}
			wg.Add(2)
			go func() {
				defer wg.Done()
				for counter1 != howManyBytes1 {
					counter1 += readT(t, conn1, b)
				}
			}()
			go func() {
				defer wg.Done()
				for counter2 != howManyBytes2 {
					counter2 += readT(t, conn2, b)
				}
			}()

			wg.Wait()

			return counter1 + counter2
		}

		checkRate(t, howManyBytes1+howManyBytes2, time.Second*5, op)
	})
}

func TestCheckDefaultSettings(t *testing.T) {
	ml := mockListener{}
	bl := NewListener(context.Background(), ml)
	gw, gr := bl.GetGlobalLimits()
	cw, cr := bl.GetConnLimits()
	unlimited := NewUnlimitedConfig()
	assert.Equal(t, unlimited, gw)
	assert.Equal(t, unlimited, gr)
	assert.Equal(t, unlimited, cw)
	assert.Equal(t, unlimited, cr)
}

func TestCheckConfigs(t *testing.T) {
	ml := mockListener{}
	bl := NewListener(context.Background(), ml)

	bl.SetGlobalLimits(NewConfig(100, -1), NewConfig(100))
	bl.SetConnLimits(NewConfig(100, 10), NewConfig(-100, 20))

	gw, gr := bl.GetGlobalLimits()
	cw, cr := bl.GetConnLimits()

	assert.Equal(t, NewConfig(100, 100), gw)
	assert.Equal(t, NewConfig(100, 100), gr)
	assert.Equal(t, NewConfig(100, 10), cw)
	assert.Equal(t, NewUnlimitedConfig(), cr)
}

// TestSetLimitsPerConnection tests simple connection rate limiter cases.
func TestSetLimitsPerConnection(tOuter *testing.T) {
	tOuter.Run("write 20 bytes in 2 seconds", func(t *testing.T) {
		t.Parallel()
		bl := NewListener(context.Background(), mockListener{})
		cw, cr := bl.GetConnLimits()
		// Set rate 10 B/s.
		howManyBytes := 20
		howManyRounds := 2
		rateBps := howManyBytes / howManyRounds
		cw = NewConfig(rate.Limit(rateBps))
		bl.SetConnLimits(cw, cr)
		conn := acceptT(t, bl)
		b := newSlice(rateBps)

		var op OperationFunc = func() int {
			counter := 0
			for i := 0; i < howManyRounds; i++ {
				n := writeT(t, conn, b)
				require.Equal(t, rateBps, n, "failed to send all bytes into connection")
				counter += n
			}

			return counter
		}

		checkRate(t, howManyBytes, time.Second*2, op)
	})

	tOuter.Run("read 20 bytes in 2 seconds", func(t *testing.T) {
		t.Parallel()
		bl := NewListener(context.Background(), mockListener{})
		cw, cr := bl.GetConnLimits()
		// Set rate 10 B/s.
		howManyBytes := 20
		howManyRounds := 2
		rateBps := howManyBytes / howManyRounds
		cr = NewConfig(rate.Limit(rateBps))
		bl.SetConnLimits(cw, cr)
		conn := acceptT(t, bl)
		b := newSlice(rateBps)

		var op OperationFunc = func() int {
			counter := 0
			for i := 0; i < howManyRounds; i++ {
				n := readT(t, conn, b)
				require.Equal(t, rateBps, n, "failed to read all bytes from connection")
				counter += n
			}

			return counter
		}

		checkRate(t, howManyBytes, time.Second*2, op)
	})

	tOuter.Run("write 20 bytes immediately, because burst is enough", func(t *testing.T) {
		t.Parallel()
		bl := NewListener(context.Background(), mockListener{})
		cw, cr := bl.GetConnLimits()
		// Set rate 10 B/s and burst to 20, so it should be able to write 20 bytes immediately.
		rateBps := 10
		burst := 20
		cw = NewConfig(rate.Limit(rateBps), burst)
		bl.SetConnLimits(cw, cr)
		conn := acceptT(t, bl)
		b := newSlice(rateBps)

		var op OperationFunc = func() int {
			counter := writeT(t, conn, b)
			counter += writeT(t, conn, b)

			return counter
		}

		checkQuickOperation(t, burst, op)
	})

	tOuter.Run("read 20 bytes immediately, because burst is enough", func(t *testing.T) {
		t.Parallel()
		bl := NewListener(context.Background(), mockListener{})
		cw, cr := bl.GetConnLimits()
		// Set rate 10 B/s and burst to 20, so it should be able to write 20 bytes immediately.
		rateBps := 10
		burst := 20
		cr = NewConfig(rate.Limit(rateBps), burst)
		bl.SetConnLimits(cw, cr)
		conn := acceptT(t, bl)
		b := newSlice(rateBps)

		var op OperationFunc = func() int {
			counter := readT(t, conn, b)
			counter += readT(t, conn, b)

			return counter
		}
		checkQuickOperation(t, burst, op)
	})

	tOuter.Run("burst exploded when reading from connection", func(t *testing.T) {
		t.Parallel()
		bl := NewListener(context.Background(), mockListener{})
		cw, cr := bl.GetConnLimits()
		// Set rate 10 B/s and burst to 20, so it should be able to write 20 bytes immediately.
		rateBps := 10
		burst := 5
		cr = NewConfig(rate.Limit(rateBps), burst)
		bl.SetConnLimits(cw, cr)
		conn := acceptT(t, bl)
		b := newSlice(rateBps)

		_, err := conn.Read(b)
		require.Error(t, err, "failed to write data into connection")
	})

	tOuter.Run("cancel while writing to connection", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())

		bl := NewListener(ctx, mockListener{})
		cw, cr := bl.GetConnLimits()
		// Set rate 2 B/s.
		rateBps := 2
		cw = NewConfig(rate.Limit(rateBps))
		bl.SetConnLimits(cw, cr)
		conn := acceptT(t, bl)
		b := newSlice(rateBps)

		go func() {
			time.Sleep(1050 * time.Millisecond)
			// Cancel context, so Write will exit with error.
			cancel()
		}()

		counter := 0
		for {
			n, err := conn.Write(b)
			counter += n
			if err != nil {
				require.ErrorIs(t, err, context.Canceled)
				break
			}
		}

		assert.Equal(t, 4, counter, "processed number of bytes is not the same")
	})

	tOuter.Run("cancel while reading from connection", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())

		bl := NewListener(ctx, mockListener{})
		cw, cr := bl.GetConnLimits()
		// Set rate 2 B/s.
		rateBps := 2
		cr = NewConfig(rate.Limit(rateBps))
		bl.SetConnLimits(cw, cr)
		conn := acceptT(t, bl)
		b := newSlice(rateBps)

		go func() {
			time.Sleep(1050 * time.Millisecond)
			// Cancel context, so Write will exit with error.
			cancel()
		}()

		counter := 0
		for {
			n, err := conn.Read(b)
			counter += n
			if err != nil {
				require.ErrorIs(t, err, context.Canceled)
				break
			}
		}

		assert.Equal(t, 4, counter, "processed number of bytes is not the same")
	})
}

// TestSetLimitsGlobal tests simple tests for global limiter.
func TestSetLimitsGlobal(tOuter *testing.T) {
	tOuter.Run("write 50 bytes in 5 seconds", func(t *testing.T) {
		t.Parallel()
		bl := NewListener(context.Background(), mockListener{})
		gw, gr := bl.GetGlobalLimits()
		// Set rate 10 B/s.
		howManyBytes := 50
		howManyRounds := 5
		rateBps := howManyBytes / howManyRounds
		gw = NewConfig(rate.Limit(rateBps))
		setGlobalLimits(bl, gw, gr)
		conn := acceptT(t, bl)
		b := newSlice(rateBps)

		var op OperationFunc = func() int {
			counter := 0
			for i := 0; i < howManyRounds; i++ {
				n := writeT(t, conn, b)
				require.Equal(t, rateBps, n, "failed to send all bytes into connection")
				counter += n
			}

			return counter
		}

		checkRate(t, howManyBytes, time.Second*5, op)
	})

	tOuter.Run("read 30 bytes in 3 seconds", func(t *testing.T) {
		t.Parallel()
		bl := NewListener(context.Background(), mockListener{})
		gw, gr := bl.GetGlobalLimits()
		// Set rate 10 B/s.
		howManyBytes := 30
		howManyRounds := 3
		rateBps := howManyBytes / howManyRounds
		gr = NewConfig(rate.Limit(rateBps))
		setGlobalLimits(bl, gw, gr)
		conn := acceptT(t, bl)
		b := newSlice(rateBps)
		var op OperationFunc = func() int {
			counter := 0
			for i := 0; i < howManyRounds; i++ {
				n := readT(t, conn, b)
				require.Equal(t, rateBps, n, "failed to read all bytes into connection")
				counter += n
			}

			return counter
		}

		checkRate(t, howManyBytes, time.Second*3, op)
	})
}

// TestGetNewConfig tests whether connections are informed about changed config.
func TestGetNewConfig(tOuter *testing.T) {
	tOuter.Run("trigger channel when connection configuration is changed", func(t *testing.T) {
		t.Parallel()
		bl := NewListener(context.Background(), mockListener{})
		cw, cr := NewConfig(10), NewConfig(20)
		oldChannel, _, _ := bl.GetConnCfgs()
		bl.SetConnLimits(cw, cr)
		newChannel, newCW, newCR := bl.GetConnCfgs()
		assert.Equal(t, cw, newCW)
		assert.Equal(t, cr, newCR)
		closed := false
		select {
		case _, ok := <-oldChannel:
			closed = !ok
		default:
		}
		assert.Equal(t, true, closed, "old channel should have been closed")

		closed = true
		select {
		case <-newChannel:
		default:
			closed = false
		}
		assert.Equal(t, false, closed, "new channel should not have been closed")
	})

	tOuter.Run("don't trigger channel when connection configuration is the same", func(t *testing.T) {
		t.Parallel()
		bl := NewListener(context.Background(), mockListener{})
		cw, cr := bl.GetConnLimits()
		// Set the same values, so configuration should not change.
		bl.SetConnLimits(cw, cr)
		newChannel, newCW, newCR := bl.GetConnCfgs()
		assert.Equal(t, cw, newCW)
		assert.Equal(t, cr, newCR)

		closed := true
		select {
		case <-newChannel:
		default:
			closed = false
		}
		assert.Equal(t, false, closed, "new channel should not have been closed")
	})
}

// TestChangeLimits changes limits on the fly.
func TestChangeLimits(tOuter *testing.T) {
	tOuter.Run("write 50 bytes in 8 seconds using 2 different global rates", func(t *testing.T) {
		t.Parallel()
		var rate1, rate2 rate.Limit = 10, 5
		howManyBytes := 50

		bl := NewListener(context.Background(), mockListener{})
		_, gr := bl.GetGlobalLimits()

		setGlobalLimits(bl, NewConfig(rate1), gr)
		conn := acceptT(t, bl)

		b := newSlice(int(rate1))
		var op OperationFunc = func() int {
			counter := 0
			i := 0
			for counter != howManyBytes {
				counter += writeT(t, conn, b)
				if i == 1 {
					// Change rate.
					bl.SetGlobalLimits(NewConfig(rate2), gr)
					b = newSlice(int(rate2))
				}
				i++
			}

			return counter
		}

		// rate is 10 bps for 2 seconds, so it should send 20 bytes.
		// then rate is 5 bps til the end, so it should send rest 30 bytes in 6 seconds.
		// Eventually it gives us 50 bytes per 8 seconds.
		checkRate(t, howManyBytes, time.Second*8, op)
	})

	tOuter.Run("read 50 bytes in 8 seconds using 2 different global rates", func(t *testing.T) {
		t.Parallel()
		var rate1, rate2 rate.Limit = 10, 5
		howManyBytes := 50

		bl := NewListener(context.Background(), mockListener{})
		gw, _ := bl.GetGlobalLimits()
		setGlobalLimits(bl, gw, NewConfig(rate1))
		conn := acceptT(t, bl)
		b := newSlice(int(rate1))

		var op OperationFunc = func() int {
			counter := 0
			i := 0
			for counter != howManyBytes {
				counter += readT(t, conn, b)
				if i == 1 {
					// Change rate.
					bl.SetGlobalLimits(gw, NewConfig(rate2))
					b = newSlice(int(rate2))
				}
				i++
			}

			return counter
		}

		// rate is 10 bps for 2 seconds, so it should read 20 bytes.
		// then rate is 5 bps til the end, so it should read rest 30 bytes in 6 seconds.
		// Eventually it gives us 50 bytes per 8 seconds.
		checkRate(t, howManyBytes, time.Second*8, op)
	})

	tOuter.Run("write 50 bytes in 8 seconds using 2 different connection rates", func(t *testing.T) {
		t.Parallel()
		var rate1, rate2 rate.Limit = 10, 5
		howManyBytes := 50

		bl := NewListener(context.Background(), mockListener{})
		_, cr := bl.GetConnLimits()
		bl.SetConnLimits(NewConfig(rate1), cr)
		conn := acceptT(t, bl)
		b := newSlice(int(rate1))

		var op OperationFunc = func() int {
			counter := 0
			i := 0
			for counter != howManyBytes {
				counter += writeT(t, conn, b)
				if i == 1 {
					// Change rate.
					bl.SetConnLimits(NewConfig(rate2), cr)
					b = newSlice(int(rate2))
				}
				i++
			}

			return counter
		}

		// rate is 10 bps for 2 seconds, so it should send 20 bytes.
		// then rate is 5 bps til the end, so it should send rest 30 bytes in 6 seconds.
		// Eventually it gives us 50 bytes per 8 seconds.
		checkRate(t, howManyBytes, time.Second*8, op)
	})

	tOuter.Run("read 50 bytes in 8 seconds using 2 different connection rates", func(t *testing.T) {
		t.Parallel()
		var rate1, rate2 rate.Limit = 10, 5
		howManyBytes := 50

		bl := NewListener(context.Background(), mockListener{})
		cw, _ := bl.GetConnLimits()
		bl.SetConnLimits(cw, NewConfig(rate1))
		conn := acceptT(t, bl)
		b := newSlice(int(rate1))

		var op OperationFunc = func() int {
			counter := 0
			i := 0
			for counter != howManyBytes {
				counter += readT(t, conn, b)
				if i == 1 {
					// Change rate.
					bl.SetConnLimits(cw, NewConfig(rate2))
					b = newSlice(int(rate2))
				}
				i++
			}

			return counter
		}

		// rate is 10 bps for 2 seconds, so it should read 20 bytes.
		// then rate is 5 bps til the end, so it should read rest 30 bytes in 6 seconds.
		// Eventually it gives us 50 bytes per 8 seconds.
		checkRate(t, howManyBytes, time.Second*8, op)
	})
}

// TestGlobalAndConnectionLimits uses mixed global and connection limiters.
func TestGlobalAndConnectionLimits(tOuter *testing.T) {
	tOuter.Run("read 50 bytes in 8 seconds using 2 different connection and global rates", func(t *testing.T) {
		t.Parallel()
		var rateConn, rateGlobal rate.Limit = 10, 5
		howManyBytes := 50

		bl := NewListener(context.Background(), mockListener{})
		cw, _ := bl.GetConnLimits()
		bl.SetConnLimits(cw, NewConfig(rateConn))
		gw, _ := bl.GetGlobalLimits()
		conn := acceptT(t, bl)
		b := newSlice(int(rateConn))

		var op OperationFunc = func() int {
			counter := 0
			i := 0
			for counter != howManyBytes {
				counter += readT(t, conn, b)
				if i == 1 {
					// Change rate.
					bl.SetGlobalLimits(gw, NewConfig(rateGlobal))
					b = newSlice(int(rateGlobal))
				}
				i++
			}

			return counter
		}

		checkRate(t, howManyBytes, time.Second*8, op)
	})

	tOuter.Run("write 50 bytes in 8 seconds using 2 different connection and global rates", func(t *testing.T) {
		t.Parallel()
		var rateConn, rateGlobal rate.Limit = 10, 5
		howManyBytes := 50

		bl := NewListener(context.Background(), mockListener{})
		_, cr := bl.GetConnLimits()
		bl.SetConnLimits(NewConfig(rateConn), cr)
		_, gr := bl.GetGlobalLimits()
		conn := acceptT(t, bl)
		b := newSlice(int(rateConn))

		var op OperationFunc = func() int {
			counter := 0
			i := 0
			for counter != howManyBytes {
				counter += writeT(t, conn, b)
				if i == 1 {
					// Change rate.
					bl.SetGlobalLimits(NewConfig(rateGlobal), gr)
					b = newSlice(int(rateGlobal))
				}
				i++
			}

			return counter
		}

		checkRate(t, howManyBytes, time.Second*8, op)
	})
}

func writeT(t *testing.T, conn net.Conn, b []byte) int {
	n, err := conn.Write(b)
	require.NoError(t, err, "failed to write data to connection")

	return n
}

func readT(t *testing.T, conn net.Conn, b []byte) int {
	n, err := conn.Read(b)
	require.NoError(t, err, "failed to read data from connection")

	return n
}

func acceptT(t *testing.T, bl *listener) net.Conn {
	conn, err := bl.Accept()
	require.NoError(t, err, "failed to accept connection")

	return conn
}

type mockListener struct{}

func (mockListener) Accept() (net.Conn, error) {
	return &mockConn{}, nil
}

type mockConn struct{}

func (mockConn) Read(b []byte) (n int, err error) {
	return len(b), nil
}

func (mockConn) Write(b []byte) (n int, err error) {
	return len(b), nil
}

func (mockConn) Close() error {
	panic("implement me")
}

func (mockConn) LocalAddr() net.Addr {
	panic("implement me")
}

func (mockConn) RemoteAddr() net.Addr {
	panic("implement me")
}

func (mockConn) SetDeadline(_ time.Time) error {
	panic("implement me")
}

func (mockConn) SetReadDeadline(_ time.Time) error {
	panic("implement me")
}

func (mockConn) SetWriteDeadline(_ time.Time) error {
	panic("implement me")
}

func (mockListener) Close() error {
	panic("implement me")
}

func (mockListener) Addr() net.Addr {
	panic("implement me")
}

// newSlice returns slice with a fixed zeroed length.
func newSlice(length int) []byte {
	return make([]byte, length)
}

// setGlobalLimits sets global limit and waits 1 second.
func setGlobalLimits(bl *listener, writeGlobal, readGlobal config) {
	bl.SetGlobalLimits(writeGlobal, readGlobal)
	/* TODO
	It must be investigated. It happens only when infinite global rate is changed.
	Why global limiter hangs for one second after change?
	So we lose 1 second. It is not a big deal because limits are not changed frequently.
	Connection limiter works perfectly fine, and immediately introduces new limit settings.
	*/
	time.Sleep(time.Second)
}

func checkQuickOperation(t *testing.T, expectedBytes int, f OperationFunc) {
	// Act. Measure operation.
	start := time.Now()
	gotBytes := f()
	seconds := time.Since(start)

	// Assert.
	assert.Equal(t, expectedBytes, gotBytes, "processed number of bytes is not the same")
	assert.Less(t, seconds, time.Second, "it must be quick operation")
}

// normalizeSeconds normalizes passed seconds.
// Sending or receiving data start at time 0, so if we want to send 30 bytes with rate 10 Bps then:
// - 10 bytes is processed at time 0.
// - next 10 bytes is processed on 1st second.
// - last 10 bytes is processed on 2nd second.
// So sending 30 bytes takes 2 second, but when we want to calculate rate then one second must be added.
func normalizeSeconds(t time.Duration) time.Duration {
	return t + time.Second
}

// checkRate checks actual rate if it is included in threshold -/+5%.
func checkRate(t *testing.T, expectedBytes int, expectedSeconds time.Duration, op OperationFunc) {
	require.Greater(t, expectedSeconds, time.Second, "operation must take more than 1 second")

	// Act. Measure operation.
	start := time.Now()
	gotBytes := op()
	seconds := normalizeSeconds(time.Since(start)).Seconds()
	//seconds := time.Since(start).Seconds()
	//// seconds must be normalized because test start processing data at 0 time (not after 1 second).
	//seconds += 1.0

	// Assert.
	assert.Equal(t, expectedBytes, gotBytes, "processed number of bytes is not the same")
	expectedBps := float64(expectedBytes) / expectedSeconds.Seconds()
	gotBps := float64(gotBytes) / seconds

	assert.Greaterf(t, gotBps, float64(expectedBps)*minThreshold,
		"bandwidth rate should not be lower then 95%% of expected rate, bytes=%d, seconds=%f", gotBytes, seconds)
	assert.Lessf(t, gotBps, float64(expectedBps)*maxThreshold,
		"bandwidth rate should not be grater then 105%% of expected rate, bytes=%d, seconds=%f", gotBytes, seconds)
}
