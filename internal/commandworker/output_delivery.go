package commandworker

import (
	"errors"
	"sync"
	"time"
)

var ErrOutputUnavailable = errors.New("command output consumer blocked or exceeded delivery budget")

// Callbacks have no cancellation contract. A process-wide slot remains held
// by a stuck callback, bounding both goroutines and retained memory across runs.
var outputDeliverySlots = make(chan struct{}, 8)

type outputDelivery struct {
	queue chan []byte
	stop  chan struct{}
	done  chan struct{}
	mu    sync.Mutex
	err   error
}

func newOutputDelivery(cb func([]byte)) (*outputDelivery, error) {
	select {
	case outputDeliverySlots <- struct{}{}:
	default:
		return nil, ErrOutputUnavailable
	}
	d := &outputDelivery{queue: make(chan []byte, 128), stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(d.done)
		defer func() {
			<-outputDeliverySlots
			if recover() != nil {
				d.fail()
			}
		}()
		for {
			select {
			case <-d.stop:
				return
			case b, open := <-d.queue:
				if !open {
					return
				}
				select {
				case <-d.stop:
					return
				default:
				}
				cb(b)
			}
		}
	}()
	return d, nil
}

func (d *outputDelivery) fail() { d.mu.Lock(); d.err = ErrOutputUnavailable; d.mu.Unlock() }
func (d *outputDelivery) write(p []byte) {
	copy := append([]byte(nil), p...)
	select {
	case d.queue <- copy:
	default:
		d.fail()
	}
}

func (d *outputDelivery) finish() error {
	close(d.queue)
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-d.done:
	case <-timer.C:
		d.fail()
	}
	close(d.stop)
	// Do not retain a whole output budget behind one abandoned callback.
	for range d.queue {
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}
