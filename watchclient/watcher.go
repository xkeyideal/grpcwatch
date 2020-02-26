package watchclient

import (
	"context"
	"sync"

	"github.com/xkeyideal/grpcwatch/grpclient"
	pb "github.com/xkeyideal/grpcwatch/watchpb"

	"go.uber.org/zap/zapcore"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Watcher struct {
	// gRPC 连接
	remote pb.WatchRPCClient

	// gRPC call options
	callOpts []grpc.CallOption

	// mu protects the grpc streams map
	mu sync.RWMutex

	// streams holds all the active grpc streams keyed by ctx value.
	streams map[string]*watchGrpcStream

	// log
	lg *zap.Logger
}

func NewWatcher(logFilename string, logLevel zapcore.Level, c *grpclient.GrpcClient) *Watcher {
	w := &Watcher{
		remote:  pb.NewWatchRPCClient(c.Conn),
		streams: make(map[string]*watchGrpcStream),
		lg:      newLogger(logFilename, logLevel),
	}

	if c != nil {
		w.callOpts = c.GetCallOpts()
	}

	return w
}

func (w *Watcher) newWatcherGrpcStream(inctx context.Context, watchID string, initReq watchStreamRequest) *watchGrpcStream {
	ctx, cancel := context.WithCancel(inctx)
	wgs := &watchGrpcStream{
		remote:   w.remote,
		callOpts: w.callOpts,
		watchID:  watchID,
		initReq:  initReq,
		ctx:      ctx,
		cancel:   cancel,
		reqc:     make(chan watchStreamRequest),
		respc:    make(chan *pb.WatchResponse),
		donec:    make(chan struct{}),
		errc:     make(chan error, 1),
		lg:       w.lg,
	}

	go wgs.run()

	return wgs
}

//Close Watch 客户端程序退出，主动断开所有的watch连接
func (w *Watcher) Close() {
	w.mu.Lock()
	streams := w.streams
	w.streams = nil
	w.mu.Unlock()

	for watchID, wgs := range streams {
		wr := &watchCancelRequest{
			watchID: watchID,
		}
		wgs.reqc <- wr
		wgs.close()
	}

	w.lg.Info("watcher close")
}

//CloseStream 业务主动断开某个watch的连接
func (w *Watcher) CloseStream(watchID string) {
	wr := &watchCancelRequest{
		watchID: watchID,
	}

	w.mu.Lock()
	if wgs, ok := w.streams[watchID]; ok {
		wgs.reqc <- wr
		wgs.close()
	}
	delete(w.streams, watchID)
	w.mu.Unlock()
}

//Watch 发起watch请求
func (w *Watcher) Watch(ctx context.Context, watchID string, app *pb.App) chan *pb.WatchResponse {
	wr := &watchCreateRequest{
		watchID: watchID,
		app:     app,
	}

	w.mu.Lock()
	if w.streams == nil {
		w.mu.Unlock()
		ch := make(chan *pb.WatchResponse)
		close(ch)
		return ch
	}

	// 这里处理不可以重复watch
	wgs := w.streams[watchID]
	if wgs != nil {
		w.mu.Unlock()
		ch := make(chan *pb.WatchResponse)
		close(ch)
		return ch
	}

	wgs = w.newWatcherGrpcStream(ctx, watchID, wr)
	w.streams[watchID] = wgs
	w.mu.Unlock()

	ok := false

	// 阻塞等待连接服务端成功，除非调用方主动结束，否则一直重试
	select {
	// 连接成功后，发送watch request
	case wgs.reqc <- wr:
		ok = true
	case <-ctx.Done():
	case <-wgs.donec:
		return w.Watch(ctx, watchID, app)
	}

	// 将watch response channel交给调用方处理
	if ok {
		return wgs.respc
	}

	// couldn't create channel; return closed channel
	closeCh := make(chan *pb.WatchResponse, 1)

	close(closeCh)
	return closeCh
}

// isHaltErr returns true if the given error and context indicate no forward
// progress can be made, even after reconnecting.
func isHaltErr(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	if err == nil {
		return false
	}
	ev, _ := status.FromError(err)
	// Unavailable codes mean the system will be right back.
	// (e.g., can't connect, lost leader)
	// Treat Internal codes as if something failed, leaving the
	// system in an inconsistent state, but retrying could make progress.
	// (e.g., failed in middle of send, corrupted frame)
	// TODO: are permanent Internal errors possible from grpc?
	return ev.Code() != codes.Unavailable && ev.Code() != codes.Internal
}

// isUnavailableErr returns true if the given error is an unavailable error
func isUnavailableErr(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if err == nil {
		return false
	}
	ev, ok := status.FromError(err)
	if ok {
		// Unavailable codes mean the system will be right back.
		// (e.g., can't connect, lost leader)
		return ev.Code() == codes.Unavailable
	}
	return false
}
