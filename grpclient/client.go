package grpclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xkeyideal/grpcwatch/grpclient/balancer"
	"github.com/xkeyideal/grpcwatch/grpclient/balancer/picker"
	"github.com/xkeyideal/grpcwatch/grpclient/balancer/resolver"

	"github.com/google/uuid"
	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
)

func init() {
	balancer.RegisterBuilder(balancer.Config{
		Policy: picker.RoundrobinBalanced,
		Name:   roundRobinBalancerName,
	})
}

type GrpcClient struct {
	Conn *grpc.ClientConn

	cfg           *GrpcClientConfig
	resolverGroup *resolver.ResolverGroup
	mu            *sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc

	callOpts []grpc.CallOption
}

// Close shuts down the client's etcd connections.
func (c *GrpcClient) Close() error {
	c.cancel()

	if c.resolverGroup != nil {
		c.resolverGroup.Close()
	}
	if c.Conn != nil {
		return c.Conn.Close()
	}
	return c.ctx.Err()
}

func (c *GrpcClient) GetCallOpts() []grpc.CallOption {
	return c.callOpts
}

// Endpoints lists the registered endpoints for the client.
func (c *GrpcClient) Endpoints() []string {
	// copy the slice; protect original endpoints from being changed
	c.mu.RLock()
	defer c.mu.RUnlock()
	eps := make([]string, len(c.cfg.Endpoints))
	copy(eps, c.cfg.Endpoints)
	return eps
}

// SetEndpoints updates client's endpoints.
func (c *GrpcClient) SetEndpoints(eps ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg.Endpoints = eps
	c.resolverGroup.SetEndpoints(eps)
}

// dialWithBalancer dials the client's current load balanced resolver group.  The scheme of the host
// of the provided endpoint determines the scheme used for all endpoints of the client connection.
func (c *GrpcClient) dialWithBalancer(ep string, dopts ...grpc.DialOption) (*grpc.ClientConn, error) {
	_, host, _ := resolver.ParseEndpoint(ep)
	target := c.resolverGroup.Target(host)
	return c.dial(target, dopts...)
}

// dial configures and dials any grpc balancer target.
func (c *GrpcClient) dial(target string, dopts ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts, err := c.dialSetupOpts(dopts...)
	if err != nil {
		return nil, fmt.Errorf("failed to configure dialer: %v", err)
	}

	opts = append(opts, c.cfg.DialOptions...)

	dctx := c.ctx
	if c.cfg.DialTimeout > 0 {
		var cancel context.CancelFunc
		dctx, cancel = context.WithTimeout(c.ctx, c.cfg.DialTimeout)
		defer cancel()

		// Is this right for cases where grpc.WithBlock() is not set on the dial options
		// 设置连接超时的时候，需要明确等连接创建成功之后
		opts = append(opts, grpc.WithBlock())
	}

	conn, err := grpc.DialContext(dctx, target, opts...)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// dialSetupOpts gives the dial opts prior to any authentication.
func (c *GrpcClient) dialSetupOpts(dopts ...grpc.DialOption) (opts []grpc.DialOption, err error) {
	if c.cfg.DialKeepAliveTime > 0 {
		params := keepalive.ClientParameters{
			Time:                c.cfg.DialKeepAliveTime,
			Timeout:             c.cfg.DialKeepAliveTimeout,
			PermitWithoutStream: c.cfg.PermitWithoutStream,
		}
		opts = append(opts, grpc.WithKeepaliveParams(params))
	}
	opts = append(opts, dopts...)

	dialer := resolver.Dialer
	opts = append(opts, grpc.WithInsecure(), grpc.WithInitialWindowSize(65536*100)) // 100*64K
	opts = append(opts, grpc.WithContextDialer(dialer))

	// 设置拦截器
	retryOpts := []grpc_retry.CallOption{
		grpc_retry.WithMax(3),
		grpc_retry.WithBackoff(grpc_retry.BackoffExponential(100 * time.Millisecond)),
		grpc_retry.WithCodes(codes.NotFound, codes.Aborted),
	}

	opts = append(opts,
		//grpc.WithStreamInterceptor(c.streamClientInterceptor(withMax(0), rrBackoff)),
		grpc.WithUnaryInterceptor(grpc_retry.UnaryClientInterceptor(retryOpts...)),
	)

	return opts, nil
}

func NewGRPCClient(cfg *GrpcClientConfig) (*GrpcClient, error) {
	ctx, cancel := context.WithCancel(context.Background())

	client := &GrpcClient{
		Conn:     nil,
		cfg:      cfg,
		ctx:      ctx,
		cancel:   cancel,
		mu:       new(sync.RWMutex),
		callOpts: defaultCallOpts,
	}

	if cfg.MaxCallSendMsgSize > 0 || cfg.MaxCallRecvMsgSize > 0 {
		if cfg.MaxCallRecvMsgSize > 0 && cfg.MaxCallSendMsgSize > cfg.MaxCallRecvMsgSize {
			return nil, fmt.Errorf("gRPC message recv limit (%d bytes) must be greater than send limit (%d bytes)", cfg.MaxCallRecvMsgSize, cfg.MaxCallSendMsgSize)
		}
		callOpts := []grpc.CallOption{
			defaultFailFast,
			defaultMaxCallSendMsgSize,
			defaultMaxCallRecvMsgSize,
		}
		if cfg.MaxCallSendMsgSize > 0 {
			callOpts[1] = grpc.MaxCallSendMsgSize(cfg.MaxCallSendMsgSize)
		}
		if cfg.MaxCallRecvMsgSize > 0 {
			callOpts[2] = grpc.MaxCallRecvMsgSize(cfg.MaxCallRecvMsgSize)
		}
		client.callOpts = callOpts
	}

	var err error

	client.resolverGroup, err = resolver.NewResolverGroup(fmt.Sprintf("client-%s", uuid.New().String()))
	if err != nil {
		client.cancel()
		return nil, err
	}

	dialEndpoint := cfg.Endpoints[0]

	client.resolverGroup.SetEndpoints(cfg.Endpoints)

	conn, err := client.dialWithBalancer(dialEndpoint, grpc.WithBalancerName(roundRobinBalancerName))
	if err != nil {
		client.cancel()
		client.resolverGroup.Close()
		return nil, err
	}

	client.Conn = conn

	return client, nil
}
