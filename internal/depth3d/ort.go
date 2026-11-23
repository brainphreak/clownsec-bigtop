// Package depth3d turns 2D video into 3D for the 3DS: a depth model (ONNX, run by ONNX Runtime on
// the GPU / Neural Engine when there is one) estimates how far each part of the picture is, and
// the two eyes are made by shifting the picture by that depth.
//
// ONNX Runtime is a native library loaded at run time (purego: no cgo, so Big Top still cross-builds
// as before): onnxruntime.dll + DirectML.dll on Windows (any DirectX 12 GPU), libonnxruntime.dylib
// on macOS (CoreML), libonnxruntime.so on Linux (CPU). Version 1.24 (ORT_API_VERSION 24): the last
// with the DirectML provider, used on every platform so the API is the same everywhere.
package depth3d

import (
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const ortAPIVersion = 24

// positions in the OrtApi function table (onnxruntime_c_api.h, struct OrtApi; append-only, so
// stable across versions >= 24)
const (
	fnGetErrorMessage         = 2
	fnCreateEnv               = 3
	fnCreateSessionFromArray  = 8
	fnRun                     = 9
	fnCreateSessionOptions    = 10
	fnSetSessionExecutionMode = 13
	fnDisableMemPattern       = 17
	fnSetSessionGraphOptLevel = 23
	fnSetIntraOpNumThreads    = 24
	fnCreateTensorWithData    = 49
	fnGetTensorMutableData    = 51
	fnCreateCpuMemoryInfo     = 69
	fnReleaseEnv              = 92
	fnReleaseStatus           = 93
	fnReleaseMemoryInfo       = 94
	fnReleaseSession          = 95
	fnReleaseValue            = 96
	fnReleaseSessionOptions   = 100
	fnAppendExecutionProvider = 216 // SessionOptionsAppendExecutionProvider(opts, name, keys, values, n)
	ortLoggingWarning         = 2
	ortEnableAll              = 99
	ortSequential             = 0
	ortDeviceAllocator        = 0
	ortMemTypeDefault         = 0
	ortTensorFloat            = 1
	coremlFlagCreateMLProgram = 0x010
)

// Runtime is ONNX Runtime, loaded once.
type Runtime struct {
	lib          uintptr
	api          uintptr
	env          uintptr
	mem          uintptr
	appendDML    uintptr // OrtSessionOptionsAppendExecutionProvider_DML (Windows DirectML build)
	appendCoreML uintptr // OrtSessionOptionsAppendExecutionProvider_CoreML (macOS)
	Version      string
	// CacheDir: where CoreML keeps the models it compiled (compiling takes ~30 s; a cached one loads
	// at once). The model must carry a COREML_CACHE_KEY metadata entry. "" = no cache.
	CacheDir string
	// Units: CoreML compute units -- "ALL" (Neural Engine + GPU + CPU), "CPUAndGPU", "CPUAndNeuralEngine"
	Units string
}

var (
	rtOnce sync.Once
	rt     *Runtime
	rtErr  error
)

// Load loads ONNX Runtime from path (the library file) once; later calls return the same one.
func Load(path string) (*Runtime, error) {
	rtOnce.Do(func() { rt, rtErr = load(path) })
	return rt, rtErr
}

func load(path string) (*Runtime, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("ONNX Runtime not found: %w", err)
	}
	lib, err := openLibrary(path)
	if err != nil {
		return nil, fmt.Errorf("can't load ONNX Runtime: %w", err)
	}
	r := &Runtime{lib: lib}
	base, err := symbol(lib, "OrtGetApiBase")
	if err != nil {
		return nil, err
	}
	b, _, _ := purego.SyscallN(base)
	if b == 0 {
		return nil, errors.New("ONNX Runtime: no API base")
	}
	getAPI := *(*uintptr)(unsafe.Pointer(b))
	getVer := *(*uintptr)(unsafe.Pointer(b + unsafe.Sizeof(uintptr(0))))
	if v, _, _ := purego.SyscallN(getVer); v != 0 {
		r.Version = cString(v)
	}
	r.api, _, _ = purego.SyscallN(getAPI, ortAPIVersion)
	if r.api == 0 {
		return nil, fmt.Errorf("ONNX Runtime %s doesn't offer API version %d", r.Version, ortAPIVersion)
	}
	r.appendDML, _ = symbol(lib, "OrtSessionOptionsAppendExecutionProvider_DML")
	r.appendCoreML, _ = symbol(lib, "OrtSessionOptionsAppendExecutionProvider_CoreML")
	var pin runtime.Pinner // everything ONNX Runtime is handed an address of stays put (Go moves stacks)
	defer pin.Unpin()
	logid := cstr("bigtop")
	pin.Pin(&logid[0])
	pin.Pin(r)
	if err := r.call(fnCreateEnv, ortLoggingWarning, ptr(logid), uintptr(unsafe.Pointer(&r.env))); err != nil {
		return nil, err
	}
	if err := r.call(fnCreateCpuMemoryInfo, ortDeviceAllocator, ortMemTypeDefault, uintptr(unsafe.Pointer(&r.mem))); err != nil {
		return nil, err
	}
	runtime.KeepAlive(logid)
	return r, nil
}

func (r *Runtime) fn(i int) uintptr {
	return *(*uintptr)(unsafe.Pointer(r.api + uintptr(i)*unsafe.Sizeof(uintptr(0))))
}

// call runs a status-returning API function: nil, or the error it reported.
func (r *Runtime) call(i int, args ...uintptr) error {
	st, _, _ := purego.SyscallN(r.fn(i), args...)
	return r.status(st)
}

func (r *Runtime) status(st uintptr) error {
	if st == 0 {
		return nil
	}
	m, _, _ := purego.SyscallN(r.fn(fnGetErrorMessage), st)
	msg := cString(m)
	purego.SyscallN(r.fn(fnReleaseStatus), st)
	return errors.New("ONNX Runtime: " + msg)
}

func (r *Runtime) release(i int, p uintptr) {
	if p != 0 {
		purego.SyscallN(r.fn(i), p)
	}
}

// Session is one loaded model with a fixed input size (fixed = fast on CoreML / DirectML).
type Session struct {
	mu      sync.Mutex // one frame at a time (several streams can share a session)
	r       *Runtime
	s       uintptr
	H, W    int    // the model's input size
	in, out []byte // tensor names (C strings)
	Device  string // what runs it: "DirectML", "CoreML", "CPU"
	buf     []float32
	x       *runScratch
	shape   [4]int64
}

// runScratch: what Run hands ONNX Runtime the addresses of
type runScratch struct {
	in, res, data          uintptr
	inNames, outNames, ins [1]uintptr
}

// NewSession loads a model (bytes of an .onnx file) taking a 1x3xHxW float picture and giving a
// 1xHxW depth map. gpu: try the platform's accelerator first.
func (r *Runtime) NewSession(model []byte, inName, outName string, h, w int, gpu bool) (*Session, error) {
	var pin runtime.Pinner
	defer pin.Unpin()
	opts := new(uintptr)
	pin.Pin(opts)
	if err := r.call(fnCreateSessionOptions, uintptr(unsafe.Pointer(opts))); err != nil {
		return nil, err
	}
	defer func() { r.release(fnReleaseSessionOptions, *opts) }()
	lvl := uintptr(ortEnableAll)
	if v := os.Getenv("DEPTH3D_OPT"); v != "" { // testing: 0 none, 1 basic, 2 extended, 99 all
		fmt.Sscan(v, &lvl)
	}
	r.call(fnSetSessionGraphOptLevel, *opts, lvl)
	r.call(fnSetIntraOpNumThreads, *opts, uintptr(max(1, runtime.NumCPU()/2)))
	dev := "CPU"
	if gpu && r.appendDML != 0 {
		// DirectML wants sequential execution and no memory pattern
		r.call(fnSetSessionExecutionMode, *opts, ortSequential)
		r.call(fnDisableMemPattern, *opts)
		st, _, _ := purego.SyscallN(r.appendDML, *opts, 0)
		if derr := r.status(st); derr == nil { // status frees st: read it once
			dev = "DirectML"
		} else {
			// no DirectX 12 graphics (or its driver is too old): the processor, and why -- shown with the
			// speed test, as a silent fallback looks like Big Top ignoring the graphics chip
			msg := strings.TrimPrefix(derr.Error(), "ONNX Runtime: ")
			if len(msg) > 120 {
				msg = msg[:120] + "…"
			}
			dev = "CPU (DirectML unavailable: " + msg + ")"
			log.Printf("depth3d: DirectML unavailable, using the processor: %v", derr)
		}
	} else if gpu && r.appendCoreML != 0 {
		if r.coreml(*opts) == nil {
			dev = "CoreML"
		} else if st, _, _ := purego.SyscallN(r.appendCoreML, *opts, coremlFlagCreateMLProgram); r.status(st) == nil {
			dev = "CoreML"
		} else {
			dev = "CPU (CoreML unavailable)"
			log.Printf("depth3d: CoreML unavailable, using the processor")
		}
	} else if gpu && runtime.GOOS == "linux" {
		dev = "CPU (no GPU support on Linux yet)"
	}
	s := &Session{r: r, H: h, W: w, in: cstr(inName), out: cstr(outName), Device: dev,
		buf: make([]float32, 3*h*w), x: &runScratch{}, shape: [4]int64{1, 3, int64(h), int64(w)}}
	pin.Pin(s)
	pin.Pin(&model[0])
	err := r.call(fnCreateSessionFromArray, r.env, uintptr(unsafe.Pointer(&model[0])), uintptr(len(model)),
		*opts, uintptr(unsafe.Pointer(&s.s)))
	runtime.KeepAlive(model)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// coreml: the CoreML provider through the generic call, which takes its options as strings -- the
// ML Program format on every compute unit (Neural Engine + GPU + CPU), and the compiled-model cache.
func (r *Runtime) coreml(opts uintptr) error {
	units := r.Units
	if units == "" {
		units = "ALL"
	}
	kv := [][2]string{{"ModelFormat", "MLProgram"}, {"MLComputeUnits", units}}
	if r.CacheDir != "" {
		os.MkdirAll(r.CacheDir, 0o755)
		kv = append(kv, [2]string{"ModelCacheDirectory", r.CacheDir})
	}
	var keep [][]byte
	keys, vals := make([]uintptr, len(kv)), make([]uintptr, len(kv))
	var pin runtime.Pinner
	defer pin.Unpin()
	for i, p := range kv {
		k, v := cstr(p[0]), cstr(p[1])
		pin.Pin(&k[0])
		pin.Pin(&v[0])
		keep = append(keep, k, v)
		keys[i], vals[i] = ptr(k), ptr(v)
	}
	name := cstr("CoreML")
	pin.Pin(&name[0])
	pin.Pin(&keys[0])
	pin.Pin(&vals[0])
	err := r.call(fnAppendExecutionProvider, opts, ptr(name), uintptr(unsafe.Pointer(&keys[0])),
		uintptr(unsafe.Pointer(&vals[0])), uintptr(len(kv)))
	runtime.KeepAlive(keep)
	runtime.KeepAlive(name)
	runtime.KeepAlive(keys)
	runtime.KeepAlive(vals)
	return err
}

// Input is the picture buffer to fill before Run: 3 planes (R, G, B) of H*W, normalised.
func (s *Session) Input() []float32 { return s.buf }

// Run estimates depth from Input() into out (H*W values, bigger = nearer).
func (s *Session) Run(out []float32) error {
	r := s.r
	x := s.x // the call's scratch, in the Session (on the heap, so it stays where ONNX Runtime writes)
	var pin runtime.Pinner
	defer pin.Unpin()
	pin.Pin(s)
	pin.Pin(x)
	pin.Pin(&s.buf[0])
	pin.Pin(&s.in[0])
	pin.Pin(&s.out[0])
	x.in, x.res, x.data = 0, 0, 0
	if err := r.call(fnCreateTensorWithData, r.mem, uintptr(unsafe.Pointer(&s.buf[0])), uintptr(len(s.buf)*4),
		uintptr(unsafe.Pointer(&s.shape[0])), 4, ortTensorFloat, uintptr(unsafe.Pointer(&x.in))); err != nil {
		return err
	}
	defer r.release(fnReleaseValue, x.in)
	x.inNames[0], x.outNames[0], x.ins[0] = ptr(s.in), ptr(s.out), x.in
	if err := r.call(fnRun, s.s, 0, uintptr(unsafe.Pointer(&x.inNames[0])), uintptr(unsafe.Pointer(&x.ins[0])), 1,
		uintptr(unsafe.Pointer(&x.outNames[0])), 1, uintptr(unsafe.Pointer(&x.res))); err != nil {
		return err
	}
	defer r.release(fnReleaseValue, x.res)
	if err := r.call(fnGetTensorMutableData, x.res, uintptr(unsafe.Pointer(&x.data))); err != nil {
		return err
	}
	copy(out, unsafe.Slice((*float32)(unsafe.Pointer(x.data)), s.H*s.W))
	return nil
}

// Close frees the model.
func (s *Session) Close() {
	if s != nil && s.s != 0 {
		s.r.release(fnReleaseSession, s.s)
		s.s = 0
	}
}

func cstr(s string) []byte { return append([]byte(s), 0) }
func ptr(b []byte) uintptr { return uintptr(unsafe.Pointer(&b[0])) }

func cString(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Pointer(p + uintptr(n))) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(p)), n))
}
