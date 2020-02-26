package main

import (
	"os"

	"github.com/xkeyideal/grpcwatch/watchserver"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func newLogger(logFilename string, level zapcore.Level) *zap.Logger {
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder, // 小写编码器
		EncodeTime:     zapcore.ISO8601TimeEncoder,       // ISO8601 UTC 时间格式
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.FullCallerEncoder, // 全路径编码器
	}

	// hook := lumberjack.Logger{
	// 	Filename:   logFilename, // 日志文件路径
	// 	MaxSize:    512,         // 每个日志文件保存的最大尺寸 单位：M
	// 	MaxBackups: 300,         // 日志文件最多保存多少个备份
	// 	MaxAge:     7,           // 文件最多保存多少天
	// 	Compress:   true,        // 是否压缩
	// }

	// 设置日志级别
	atomicLevel := zap.NewAtomicLevel()
	atomicLevel.SetLevel(level)

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),                                          // 编码器配置
		zapcore.NewMultiWriteSyncer(zapcore.Lock(os.Stdout) /*, zapcore.AddSync(&hook)*/), // 打印到控制台和文件
		atomicLevel, // 日志级别
	)

	logger := zap.New(core /*, zap.AddCaller()*/)

	return logger
}

func main() {
	cfg := &watchserver.GrpcServerConfig{
		Port:                  5853,
		MaxConnectionIdle:     20,
		PingInterval:          1,
		Timeout:               3,
		KeepAliveMinTime:      1,
		MaxConnectionAge:      120,
		MaxConnectionAgeGrace: 5,
		WriteBufferSize:       2 * 1024 * 1024, //2MB
		ReadBufferSize:        2 * 1024 * 1024,
		MaxRecvMsgSize:        12 * 1024 * 1024,
		MaxSendMsgSize:        12 * 1024 * 1024,
		MaxConcurrentStreams:  655360,
	}

	lg := newLogger("", zapcore.DebugLevel)
	err := watchserver.NewGrpcServer(cfg, lg)
	if err != nil {
		panic(err)
	}
}
