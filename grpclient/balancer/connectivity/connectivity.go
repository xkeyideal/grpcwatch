package connectivity

import (
	"sync"

	"google.golang.org/grpc/connectivity"
)

// Recorder records gRPC connectivity.
type Recorder interface {
	GetCurrentState() connectivity.State
	RecordTransition(oldState, newState connectivity.State)
}

// New returns a new Recorder.
func New() Recorder {
	return &recorder{}
}

// recorder takes the connectivity states of multiple SubConns
// and returns one aggregated connectivity state.
// ref. https://github.com/grpc/grpc-go/blob/master/balancer/balancer.go
type recorder struct {
	mu sync.RWMutex

	cur connectivity.State

	numReady            uint64 // Number of addrConns in ready state.
	numConnecting       uint64 // Number of addrConns in connecting state.
	numTransientFailure uint64 // Number of addrConns in transientFailure.
}

func (rc *recorder) GetCurrentState() (state connectivity.State) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.cur
}

// RecordTransition records state change happening in subConn and based on that
// it evaluates what aggregated state should be.
//
//  - If at least one SubConn in Ready, the aggregated state is Ready;
//  - Else if at least one SubConn in Connecting, the aggregated state is Connecting;
//  - Else the aggregated state is TransientFailure.
//
// Idle and Shutdown are not considered.
//
// ref. https://github.com/grpc/grpc-go/blob/master/balancer/balancer.go
func (rc *recorder) RecordTransition(oldState, newState connectivity.State) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	for idx, state := range []connectivity.State{oldState, newState} {
		updateVal := 2*uint64(idx) - 1 // -1 for oldState and +1 for new.
		switch state {
		case connectivity.Ready:
			rc.numReady += updateVal
		case connectivity.Connecting:
			rc.numConnecting += updateVal
		case connectivity.TransientFailure:
			rc.numTransientFailure += updateVal
		default:
		}
	}

	switch { // must be exclusive, no overlap
	case rc.numReady > 0:
		rc.cur = connectivity.Ready
	case rc.numConnecting > 0:
		rc.cur = connectivity.Connecting
	default:
		rc.cur = connectivity.TransientFailure
	}
}
