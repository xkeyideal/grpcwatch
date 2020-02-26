package balancer

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xkeyideal/grpcwatch/grpclient/balancer/connectivity"
	"github.com/xkeyideal/grpcwatch/grpclient/balancer/picker"

	"google.golang.org/grpc/balancer"
	grpcconnectivity "google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/resolver"
	_ "google.golang.org/grpc/resolver/dns"         // register DNS resolver
	_ "google.golang.org/grpc/resolver/passthrough" // register passthrough resolver
)

// Config defines balancer configurations.
type Config struct {
	// Policy configures balancer policy.
	Policy picker.Policy

	// Picker implements gRPC picker.
	// Leave empty if "Policy" field is not custom.
	// TODO: currently custom policy is not supported.
	// Picker picker.Picker

	// Name defines an additional name for balancer.
	// Useful for balancer testing to avoid register conflicts.
	// If empty, defaults to policy name.
	Name string
}

// RegisterBuilder creates and registers a builder. Since this function calls balancer.Register, it
// must be invoked at initialization time.
func RegisterBuilder(cfg Config) {
	bb := &builder{cfg}
	balancer.Register(bb)
}

type builder struct {
	cfg Config
}

// Build is called initially when creating "ccBalancerWrapper".
// "grpc.Dial" is called to this client connection.
// Then, resolved addresses will be handled via "HandleResolvedAddrs".
func (b *builder) Build(cc balancer.ClientConn, opt balancer.BuildOptions) balancer.Balancer {
	bb := &baseBalancer{
		id:     strconv.FormatInt(time.Now().UnixNano(), 36),
		policy: b.cfg.Policy,
		name:   b.cfg.Name,

		addrToSc: make(map[resolver.Address]balancer.SubConn),
		scToAddr: make(map[balancer.SubConn]resolver.Address),
		scToSt:   make(map[balancer.SubConn]grpcconnectivity.State),

		currentConn:          nil,
		connectivityRecorder: connectivity.New(),

		// initialize picker always returns "ErrNoSubConnAvailable"
		picker: picker.NewErr(balancer.ErrNoSubConnAvailable),
	}

	// TODO: support multiple connections
	bb.mu.Lock()
	bb.currentConn = cc
	bb.mu.Unlock()

	return bb
}

// Name implements "grpc/balancer.Builder" interface.
func (b *builder) Name() string { return b.cfg.Name }

// Balancer defines client balancer interface.
type Balancer interface {
	// Balancer is called on specified client connection. Client initiates gRPC
	// connection with "grpc.Dial(addr, grpc.WithBalancerName)", and then those resolved
	// addresses are passed to "grpc/balancer.Balancer.HandleResolvedAddrs".
	// For each resolved address, balancer calls "balancer.ClientConn.NewSubConn".
	// "grpc/balancer.Balancer.HandleSubConnStateChange" is called when connectivity state
	// changes, thus requires failover logic in this method.
	balancer.Balancer

	// Picker calls "Pick" for every client request.
	picker.Picker
}

type baseBalancer struct {
	id     string
	policy picker.Policy
	name   string

	mu sync.RWMutex

	addrToSc map[resolver.Address]balancer.SubConn
	scToAddr map[balancer.SubConn]resolver.Address
	scToSt   map[balancer.SubConn]grpcconnectivity.State

	currentConn          balancer.ClientConn
	connectivityRecorder connectivity.Recorder

	picker picker.Picker
}

// HandleResolvedAddrs implements "grpc/balancer.Balancer" interface.
// gRPC sends initial or updated resolved addresses from "Build".
func (bb *baseBalancer) HandleResolvedAddrs(addrs []resolver.Address, err error) {
	if err != nil {
		return
	}

	//fmt.Printf("balancer resolved, picker: %s, addresses: %+v\n", bb.picker.String(), addrsToStrings(addrs))

	bb.mu.Lock()
	defer bb.mu.Unlock()

	resolved := make(map[resolver.Address]struct{})
	for _, addr := range addrs {
		resolved[addr] = struct{}{}
		if _, ok := bb.addrToSc[addr]; !ok {
			sc, err := bb.currentConn.NewSubConn([]resolver.Address{addr}, balancer.NewSubConnOptions{})
			if err != nil {
				continue
			}
			bb.addrToSc[addr] = sc
			bb.scToAddr[sc] = addr
			bb.scToSt[sc] = grpcconnectivity.Idle
			sc.Connect() // 此方法是调用每个address，去建立连接, 最终会调用HandleSubConnStateChange
		}
	}

	for addr, sc := range bb.addrToSc {
		if _, ok := resolved[addr]; !ok {
			// was removed by resolver or failed to create subconn
			bb.currentConn.RemoveSubConn(sc)
			delete(bb.addrToSc, addr)

			// Keep the state of this sc in bb.scToSt until sc's state becomes Shutdown.
			// The entry will be deleted in HandleSubConnStateChange.
			// (DO NOT) delete(bb.scToAddr, sc)
			// (DO NOT) delete(bb.scToSt, sc)
		}
	}
}

// HandleSubConnStateChange implements "grpc/balancer.Balancer" interface.
func (bb *baseBalancer) HandleSubConnStateChange(sc balancer.SubConn, s grpcconnectivity.State) {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	old, ok := bb.scToSt[sc]
	if !ok {
		return
	}

	bb.scToSt[sc] = s
	switch s { // s的初始状态为connecting
	case grpcconnectivity.Idle:
		sc.Connect()
	case grpcconnectivity.Shutdown:
		// When an address was removed by resolver, b called RemoveSubConn but
		// kept the sc's state in scToSt. Remove state for this sc here.
		delete(bb.scToAddr, sc)
		delete(bb.scToSt, sc)
	}

	oldAggrState := bb.connectivityRecorder.GetCurrentState()
	bb.connectivityRecorder.RecordTransition(old, s)

	//fmt.Printf("HandleSubConnStateChange, state: %+v, oldstate: %+v\n", s, old)

	// Update balancer picker when one of the following happens:
	//  - this sc became ready from not-ready
	//  - this sc became not-ready from ready
	//  - the aggregated state of balancer became TransientFailure from non-TransientFailure
	//  - the aggregated state of balancer became non-TransientFailure from TransientFailure
	if (s == grpcconnectivity.Ready) != (old == grpcconnectivity.Ready) ||
		(bb.connectivityRecorder.GetCurrentState() == grpcconnectivity.TransientFailure) != (oldAggrState == grpcconnectivity.TransientFailure) {
		bb.updatePicker()
	}

	// 通知grpc balancer picker
	bb.currentConn.UpdateBalancerState(bb.connectivityRecorder.GetCurrentState(), bb.picker)
}

func (bb *baseBalancer) updatePicker() {
	if bb.connectivityRecorder.GetCurrentState() == grpcconnectivity.TransientFailure {
		bb.picker = picker.NewErr(balancer.ErrTransientFailure)
		return
	}

	// only pass ready subconns to picker
	scToAddr := make(map[balancer.SubConn]resolver.Address)
	for addr, sc := range bb.addrToSc {
		if st, ok := bb.scToSt[sc]; ok && st == grpcconnectivity.Ready {
			scToAddr[sc] = addr
		}
	}

	// 新建balancer的picker
	bb.picker = picker.New(picker.Config{
		Policy:                   bb.policy,
		SubConnToResolverAddress: scToAddr,
	})
}

// Close implements "grpc/balancer.Balancer" interface.
// Close is a nop because base balancer doesn't have internal state to clean up,
// and it doesn't need to call RemoveSubConn for the SubConns.
func (bb *baseBalancer) Close() {
	// TODO
}

func scToString(sc balancer.SubConn) string {
	return fmt.Sprintf("%p", sc)
}

func scsToStrings(scs map[balancer.SubConn]resolver.Address) (ss []string) {
	ss = make([]string, 0, len(scs))
	for sc, a := range scs {
		ss = append(ss, fmt.Sprintf("%s (%s)", a.Addr, scToString(sc)))
	}
	sort.Strings(ss)
	return ss
}

func addrsToStrings(addrs []resolver.Address) (ss []string) {
	ss = make([]string, len(addrs))
	for i := range addrs {
		ss[i] = addrs[i].Addr
	}
	sort.Strings(ss)
	return ss
}

func epsToAddrs(eps ...string) (addrs []resolver.Address) {
	addrs = make([]resolver.Address, 0, len(eps))
	for _, ep := range eps {
		u, err := url.Parse(ep)
		if err != nil {
			addrs = append(addrs, resolver.Address{Addr: ep, Type: resolver.Backend})
			continue
		}
		addrs = append(addrs, resolver.Address{Addr: u.Host, Type: resolver.Backend})
	}
	return addrs
}

var genN = new(uint32)

func genName() string {
	now := time.Now().UnixNano()
	return fmt.Sprintf("%X%X", now, atomic.AddUint32(genN, 1))
}
