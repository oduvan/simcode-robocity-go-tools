// Package engine loads the REAL Robot City game engine — the exact c-shared
// library the server runs — and drives it one tick at a time over cgo.
//
// The server compiles the stateless engineapi.Process (game/engineapi) to a
// c-shared library exporting two C-ABI symbols:
//
//	char* EngineTick(char* reqJSON, int len)  // one deterministic tick; malloc'd JSON
//	void  EngineFree(char* p)                 // free the buffer EngineTick returned
//
// We dlopen that library at runtime (so a plain `go build` needs no linked engine)
// and call it: JSON request in ⇒ JSON response out. There is NO re-implementation of
// the game here — the rules, world generation, and event timing are the server's own
// binary. The library is glibc-linked, so it loads on a glibc host (golang:*/-slim,
// Debian); musl/alpine cannot dlopen it.
package engine

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>

typedef char* (*tickfn)(char*, int);
typedef void  (*freefn)(char*);

static void* dl;
static tickfn tickp;
static freefn freep;

static int loadlib(const char* path) {
    dl = dlopen(path, RTLD_NOW|RTLD_LOCAL);
    if (!dl) return 1;
    tickp = (tickfn)dlsym(dl, "EngineTick");
    freep = (freefn)dlsym(dl, "EngineFree");
    if (!tickp || !freep) return 2;
    return 0;
}
static char* calltick(char* req, int n) { return tickp(req, n); }
static void  callfree(char* p) { freep(p); }
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// CanonicalSeed is the module's canonical world seed — every city of this type shares
// the identical map, so a seedless local run reproduces the same world (matches
// game/modules/robot_city's canonical seed and the Python tool's default).
const CanonicalSeed = 7

var (
	mu     sync.Mutex
	loaded bool
)

// Load dlopen's the engine c-shared library at path and resolves EngineTick /
// EngineFree. It is idempotent: after the first successful load, later calls (with
// any path) are no-ops — the process holds a single engine. dlopen'ing a second
// library would not re-bind the C globals, so we deliberately keep one.
func Load(path string) error {
	mu.Lock()
	defer mu.Unlock()
	if loaded {
		return nil
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if rc := C.loadlib(cpath); rc != 0 {
		if rc == 2 {
			return fmt.Errorf("engine library %q is missing EngineTick/EngineFree", path)
		}
		return fmt.Errorf("could not dlopen engine library %q (is it a glibc .so for this platform?)", path)
	}
	loaded = true
	return nil
}

// Loaded reports whether an engine library has been loaded.
func Loaded() bool {
	mu.Lock()
	defer mu.Unlock()
	return loaded
}

// Tick runs one deterministic engine tick: it hands the request JSON to EngineTick,
// copies the returned NUL-terminated JSON out, frees the engine-owned buffer, and
// returns the response bytes. An {"error": "..."} response is surfaced as a Go error.
func Tick(req []byte) ([]byte, error) {
	mu.Lock()
	defer mu.Unlock()
	if !loaded {
		return nil, errors.New("engine library not loaded; call engine.Load first")
	}
	// C.CString copies req into a NUL-terminated C buffer; the engine reads len(req)
	// bytes (the explicit length), so an embedded NUL (JSON never has one) is moot.
	creq := C.CString(string(req))
	defer C.free(unsafe.Pointer(creq))

	ptr := C.calltick(creq, C.int(len(req)))
	if ptr == nil {
		return nil, errors.New("EngineTick returned NULL")
	}
	out := C.GoString(ptr) // copy the malloc'd JSON out …
	C.callfree(ptr)        // … then release the engine-owned buffer (no leak)

	// Surface an {"error": ...} response as a Go error, matching the Python wrapper.
	var probe struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(out), &probe) == nil && probe.Error != "" {
		return nil, fmt.Errorf("engine error: %s", probe.Error)
	}
	return []byte(out), nil
}
