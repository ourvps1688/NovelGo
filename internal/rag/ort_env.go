//go:build cgo

package rag

import (
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// ortEnvOnce 保证 ONNX Runtime 全局环境只初始化一次。
// ort.InitializeEnvironment 在已初始化时会返回错误（非幂等），而 embedder 的异步
// 加载需环境就绪。用 sync.Once + IsInitialized 守卫彻底消除重复初始化的竞态。
var ortEnvOnce sync.Once

// ensureOrtEnv 幂等地初始化 ORT 全局环境。可被 embedder 的初始化路径并发/重复调用：
// 首个调用真正初始化，其余调用等待其完成后直接返回。
func ensureOrtEnv() error {
	var initErr error
	ortEnvOnce.Do(func() {
		if ort.IsInitialized() {
			return
		}
		initErr = ort.InitializeEnvironment()
	})
	return initErr
}
