package picker

import (
	"context"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
)

// newRoundrobinBalanced returns a new roundrobin balanced picker.
func newRoundrobinBalanced(cfg Config) Picker {
	scs := make([]balancer.SubConn, 0, len(cfg.SubConnToResolverAddress))
	for sc := range cfg.SubConnToResolverAddress {
		scs = append(scs, sc)
	}
	return &rrBalanced{
		p:        RoundrobinBalanced,
		scs:      scs,
		scToAddr: cfg.SubConnToResolverAddress,
	}
}

type rrBalanced struct {
	p        Policy
	mu       sync.RWMutex
	next     int
	scs      []balancer.SubConn
	scToAddr map[balancer.SubConn]resolver.Address
}

func (rb *rrBalanced) String() string { return rb.p.String() }

// Pick is called for every client request.
func (rb *rrBalanced) Pick(ctx context.Context, opts balancer.PickOptions) (balancer.SubConn, func(balancer.DoneInfo), error) {
	rb.mu.RLock()
	n := len(rb.scs)
	rb.mu.RUnlock()
	if n == 0 {
		return nil, nil, balancer.ErrNoSubConnAvailable
	}

	rb.mu.Lock()
	cur := rb.next
	sc := rb.scs[cur]
	//picked := rb.scToAddr[sc].Addr
	rb.next = (rb.next + 1) % len(rb.scs)
	rb.mu.Unlock()

	//fmt.Printf("balancer done2, address: %s, opts: %+v\n", picked, opts)

	doneFunc := func(info balancer.DoneInfo) {
		// TODO: error handling?
		if info.Err == nil {
			//fmt.Printf("balancer done, address: %s\n", picked)
		} else {
			//fmt.Println("balancer failed", info.Err)
		}
	}
	return sc, doneFunc, nil
}
