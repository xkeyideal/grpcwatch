package watchclient

import (
	pb "github.com/xkeyideal/grpcwatch/watchpb"
)

// watchStreamRequest 定义watch request的各类接口
type watchStreamRequest interface {
	toPB() *pb.WatchRequest
}

// 常规的watch参数
type watchCreateRequest struct {
	watchID string
	app     *pb.App
}

func (wcr *watchCreateRequest) toPB() *pb.WatchRequest {
	req := &pb.WatchCreateRequest{
		WatchId: wcr.watchID,
		App:     wcr.app,
	}
	cr := &pb.WatchRequest_CreateRequest{CreateRequest: req}
	return &pb.WatchRequest{RequestUnion: cr}
}

type watchCancelRequest struct {
	watchID string
}

func (wcr *watchCancelRequest) toPB() *pb.WatchRequest {
	req := &pb.WatchCancelRequest{
		WatchId: wcr.watchID,
	}
	cr := &pb.WatchRequest_CancelRequest{CancelRequest: req}
	return &pb.WatchRequest{RequestUnion: cr}
}
