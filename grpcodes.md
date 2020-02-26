## GRPC Codes的各类情况总结

客户端在创建连接的时候会出现如下几类错误：

1. Canceled resolveAddress出现出错, 连接取消
2. Unavailable 创建连接失败的时候
3. Internal IO错误
4. DeadlineExceeded, 连接超时


```go
// toRPCErr converts an error into an error from the status package.
func toRPCErr(err error) error {
	if err == nil || err == io.EOF {
		return err
	}
	if err == io.ErrUnexpectedEOF {
		return status.Error(codes.Internal, err.Error())
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	switch e := err.(type) {
	case transport.ConnectionError:
		return status.Error(codes.Unavailable, e.Desc)
	default:
		switch err {
		case context.DeadlineExceeded:
			return status.Error(codes.DeadlineExceeded, err.Error())
		case context.Canceled:
			return status.Error(codes.Canceled, err.Error())
		}
	}
	return status.Error(codes.Unknown, err.Error())
}
```

## 错误码的使用

```go
import (
    "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// 服务端返回错误码, 第一个参数：错误码，第二个参数：错误的详细信息
status.Error(codes.NotFound, "xxx")

// 客户端解析GRPC标准错误
// 如果ok == false, 那么说明，服务端没有按照GRPC的标准返回cuowu
ev, ok := status.FromError(err)
if ok {
    ev.Code()
}

// 客户端更详细的解析GRPC错误可以参考文档：https://godoc.org/google.golang.org/grpc/status
```

## 错误码归类

下述总结，只局限于自己的总结使用建议

业务错误码： InvalidArgument, NotFound, AlreadyExists, PermissionDenied, ResourceExhausted, Aborted, OutOfRange, DataLoss, Unimplemented

系统错误码： Canceled, DeadlineExceeded, FailedPrecondition, Internal, Unavailable, Unauthenticated

例如：Unimplemented 也可以作为rpc接口未实现的错误返回，此时属于系统错误码

     Unknown 任何未知错误都可以使用，不局限于业务或系统
     
     FailedPrecondition 业务请求时不能match相关条件可以使用，此时属于业务错误码，但慎用，建议详细归类至其他的错误码

建议加入retry中间件的错误码： Canceled, DeadlineExceeded, Internal, Unavailable

```go
    // OK is returned on success.
    // 不能用于错误是返回的code码，否则服务端会报错
    OK  Code = 0

    // Canceled indicates the operation was canceled (typically by the caller).
    // 请求被中途取消，一般情况下比较难出现，不能用于业务的错误码
    Canceled Code = 1

    // Unknown error. An example of where this error may be returned is
    // if a Status value received from another address space belongs to
    // an error-space that is not known in this address space. Also
    // errors raised by APIs that do not return enough error information
    // may be converted to this error.
    // 网络处理中也可能出现，业务错误码可以使用，一般不建议
    Unknown Code = 2

    // InvalidArgument indicates client specified an invalid argument.
    // Note that this differs from FailedPrecondition. It indicates arguments
    // that are problematic regardless of the state of the system
    // (e.g., a malformed file name).
    // 业务错误码，非法的参数
    InvalidArgument Code = 3

    // DeadlineExceeded means operation expired before completion.
    // For operations that change the state of the system, this error may be
    // returned even if the operation has completed successfully. For
    // example, a successful response from a server could have been delayed
    // long enough for the deadline to expire.
    // 客户端或服务端处理超时，与配置参数相关，不能用于业务错误码
    DeadlineExceeded Code = 4

    // NotFound means some requested entity (e.g., file or directory) was
    // not found.
    // 服务端未找到请求的资源，业务错误码
    NotFound Code = 5

    // AlreadyExists means an attempt to create an entity failed because one
    // already exists.
    // 创建的请求资源已经存在，业务错误码
    AlreadyExists Code = 6

    // PermissionDenied indicates the caller does not have permission to
    // execute the specified operation. It must not be used for rejections
    // caused by exhausting some resource (use ResourceExhausted
    // instead for those errors). It must not be
    // used if the caller cannot be identified (use Unauthenticated
    // instead for those errors).
    // 客户端没有权限，业务错误码
    PermissionDenied Code = 7

    // ResourceExhausted indicates some resource has been exhausted, perhaps
    // a per-user quota, or perhaps the entire file system is out of space.
    // 服务端提供的资源配额已用光，一般用于限速等错误，业务错误码
    ResourceExhausted Code = 8

    // FailedPrecondition indicates operation was rejected because the
    // system is not in a state required for the operation's execution.
    // For example, directory to be deleted may be non-empty, an rmdir
    // operation is applied to a non-directory, etc.
    //
    // A litmus test that may help a service implementor in deciding
    // between FailedPrecondition, Aborted, and Unavailable:
    //  (a) Use Unavailable if the client can retry just the failing call.
    //  (b) Use Aborted if the client should retry at a higher-level
    //      (e.g., restarting a read-modify-write sequence).
    //  (c) Use FailedPrecondition if the client should not retry until
    //      the system state has been explicitly fixed. E.g., if an "rmdir"
    //      fails because the directory is non-empty, FailedPrecondition
    //      should be returned since the client should not retry unless
    //      they have first fixed up the directory by deleting files from it.
    //  (d) Use FailedPrecondition if the client performs conditional
    //      REST Get/Update/Delete on a resource and the resource on the
    //      server does not match the condition. E.g., conflicting
    //      read-modify-write on the same resource.
    // 客户端的请求操作被拒绝，可细分为Unavailable, Aborted, FailedPrecondition，不建议作为业务错误码
    FailedPrecondition Code = 9

    // Aborted indicates the operation was aborted, typically due to a
    // concurrency issue like sequencer check failures, transaction aborts,
    // etc.
    //
    // See litmus test above for deciding between FailedPrecondition,
    // Aborted, and Unavailable.
    // 服务端处理并发请求时，突然中止，比如预见非常规错误，数据库事务中止， 业务错误码
    Aborted Code = 10

    // OutOfRange means operation was attempted past the valid range.
    // E.g., seeking or reading past end of file.
    //
    // Unlike InvalidArgument, this error indicates a problem that may
    // be fixed if the system state changes. For example, a 32-bit file
    // system will generate InvalidArgument if asked to read at an
    // offset that is not in the range [0,2^32-1], but it will generate
    // OutOfRange if asked to read from an offset past the current
    // file size.
    //
    // There is a fair bit of overlap between FailedPrecondition and
    // OutOfRange. We recommend using OutOfRange (the more specific
    // error) when it applies so that callers who are iterating through
    // a space can easily look for an OutOfRange error to detect when
    // they are done.
    // 数组越界等，业务错误码
    OutOfRange Code = 11

    // Unimplemented indicates operation is not implemented or not
    // supported/enabled in this service.
    // 客户端的请求，服务端尚未实现，业务错误码
    Unimplemented Code = 12

    // Internal errors. Means some invariants expected by underlying
    // system has been broken. If you see one of these errors,
    // something is very broken.
    // 内部错误，比如IO等，不建议作为业务错误码
    Internal Code = 13

    // Unavailable indicates the service is currently unavailable.
    // This is a most likely a transient condition and may be corrected
    // by retrying with a backoff. Note that it is not always safe to retry
    // non-idempotent operations.
    //
    // See litmus test above for deciding between FailedPrecondition,
    // Aborted, and Unavailable.
    // 网络不可达等，不建议作为业务错误码
    Unavailable Code = 14

    // DataLoss indicates unrecoverable data loss or corruption.
    // 数据缺失，业务错误码
    DataLoss Code = 15

    // Unauthenticated indicates the request does not have valid
    // authentication credentials for the operation.
    // 未被授权，业务错误码
    Unauthenticated Code = 16
```