## gRPC watch

本项目是参考[Etcd](https://github.com/etcd-io/etcd) 的watch client实现的常规业务的简易化watch 用例.

### watchclient目录

gRPC watch 的客户端核心程序，代码里有比较详细的中文注释，实现的功能：

1. 断线重连
2. gRPC stream 管理
3. 错误处理

核心功能都是参考Etcd的 clientv3/watch.go 中代码实现。

### watchserver目录

gRPC watch 的服务端核心程序，用于mock数据，代码比较简单

### grpclient 目录

gRPC client的负载均衡与分布式锁、选主的封装。

此部分代码也是基于Etcd的clientv3代码中修改。

负载均衡代码只适用于 gRPC 1.24.0版本，现有的1.27.+版本由于API的变化，导致不能使用，后续修改。

### 测试代码 test目录

1. go run server.go, 启动服务端
2. go run client.go, 启动客户端
   
服务端与客户端测试代码比较简单，可以根据自己需求修改，测试各类功能。

### 备注

由于个人能力有限，此用例中可能存在若干bug，还请鉴别使用。

最后给出 [gRPC Codes](https://github.com/xkeyideal/grpcwatch/blob/master/grocodes.md)的个人理解，仅供参考。