//go:build cgo

package rag

import (
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// ortEnvOnce 保证 ONNX Runtime 全局环境只初始化一次。
// ort.InitializeEnvironment 在已初始化时会返回错误（非幂等），而本项目会并行
// 加载 embedder 与 reranker 两个模型，各自 goroutine 都需环境就绪。若两个 goroutine
// 各自直接调用 InitializeEnvironment，后到者必失败 → reranker 静默失效。
// 用 sync.Once + IsInitialized 守卫彻底消除该竞态。
var ortEnvOnce sync.Once

// ensureOrtEnv 幂等地初始化 ORT 全局环境。可安全被 embedder / reranker 的初始化
// 路径并发调用：首个调用真正初始化，其余调用等待其完成后直接返回。
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
