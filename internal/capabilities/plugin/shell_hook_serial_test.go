package plugin

import "sync"

// shellHookSerial limits concurrent shell hook subprocess spawns in tests.
// Under -race with full-module parallelism, many short scripts can miss the
// default timeout when process start is starved.
var shellHookSerial sync.Mutex

func withSerialShellHooks(fn func()) {
	shellHookSerial.Lock()
	defer shellHookSerial.Unlock()
	fn()
}
