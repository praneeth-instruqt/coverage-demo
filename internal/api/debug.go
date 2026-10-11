package api

import (
	"fmt"
	"net/http"
	"runtime"
	"time"
)

// startedAt is when the process loaded this package, used to report uptime.
var startedAt = time.Now()

// debugInfo is the JSON shape of the debug endpoint.
type debugInfo struct {
	GoVersion  string `json:"go_version"`
	Goroutines int    `json:"goroutines"`
	HeapAlloc  uint64 `json:"heap_alloc_bytes"`
	NumGC      uint32 `json:"num_gc"`
	Uptime     string `json:"uptime"`
}

// DebugHandler reports runtime stats of the process. It is meant for local
// troubleshooting and is not mounted on the public router.
func DebugHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		writeJSON(w, http.StatusOK, debugInfo{
			GoVersion:  runtime.Version(),
			Goroutines: runtime.NumGoroutine(),
			HeapAlloc:  m.HeapAlloc,
			NumGC:      m.NumGC,
			Uptime:     time.Since(startedAt).Round(time.Second).String(),
		})
	}
}

func PrintSomething() {
	fmt.Println("print something")
}
