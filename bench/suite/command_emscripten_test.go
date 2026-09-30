package wagobench

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wago-org/wago"
)

const emscriptenErrnoNosys = -52

// addWagoEmscriptenImports supplies the small, fail-closed compatibility layer
// used by command-style Emscripten modules that otherwise rely on WASI for
// streams. Filesystem syscalls are deliberately rejected: admitted workloads
// use stdin/stdout, so broadening host access cannot happen accidentally.
func addWagoEmscriptenImports(imports *wago.Imports) {
	imports.HostFunc("env", "exit", func(code int32) { panic(wago.HostExit{Code: code}) })
	imports.HostFunc("env", "abort", func() { panic(wago.HostTrap{Err: fmt.Errorf("Emscripten abort")}) })
	imports.HostFunc("env", "__assert_fail", func(wago.HostCall) {
		panic(wago.HostTrap{Err: fmt.Errorf("Emscripten assertion failed")})
	}).Params(wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32)
	imports.HostFunc("env", "emscripten_resize_heap", func(int32) int32 { return 0 })
	imports.HostFunc("env", "emscripten_memcpy_big", func(caller wago.Caller, call wago.HostCall) {
		dst, src, n := uint32(call.I32(0)), uint32(call.I32(1)), uint32(call.I32(2))
		mem := caller.Memory()
		if !validEmscriptenRange(mem, dst, n) || !validEmscriptenRange(mem, src, n) {
			panic(wago.HostTrap{Err: fmt.Errorf("Emscripten memcpy out of bounds")})
		}
		copy(mem[dst:dst+n], mem[src:src+n])
		call.SetI32(0, int32(dst))
	}).Params(wago.ValI32, wago.ValI32, wago.ValI32).Results(wago.ValI32)
	imports.HostFunc("env", "setTempRet0", func(int32) {})

	for _, name := range []string{"__sys_open", "__sys_fcntl64", "__sys_ioctl"} {
		imports.HostFunc("env", name, func(call wago.HostCall) { call.SetI32(0, emscriptenErrnoNosys) }).
			Params(wago.ValI32, wago.ValI32, wago.ValI32).Results(wago.ValI32)
	}
	imports.HostFunc("env", "__sys_chmod", func(int32, int32) int32 { return emscriptenErrnoNosys })
	imports.HostFunc("env", "__sys_umask", func(int32) int32 { return emscriptenErrnoNosys })
	imports.HostFunc("env", "__sys_fchmod", func(int32, int32) int32 { return emscriptenErrnoNosys })
	for _, name := range []string{"__sys_lstat64", "__sys_stat64", "__sys_rename"} {
		imports.HostFunc("env", name, func(int32, int32) int32 { return emscriptenErrnoNosys })
	}
	imports.HostFunc("env", "__sys_fstat64", func(caller wago.Caller, call wago.HostCall) {
		call.SetI32(0, emscriptenFstat64(caller.Memory(), uint32(call.I32(0)), uint32(call.I32(1))))
	}).Params(wago.ValI32, wago.ValI32).Results(wago.ValI32)
	for _, name := range []string{"__sys_readlink", "__sys_fchown32", "__sys_chown32"} {
		imports.HostFunc("env", name, func(call wago.HostCall) { call.SetI32(0, emscriptenErrnoNosys) }).
			Params(wago.ValI32, wago.ValI32, wago.ValI32).Results(wago.ValI32)
	}
	imports.HostFunc("env", "__clock_gettime", func(int32, int32) int32 { return emscriptenErrnoNosys })
	imports.HostFunc("env", "__sys_unlink", func(int32) int32 { return emscriptenErrnoNosys })
	imports.HostFunc("env", "emscripten_fd_seek", func(call wago.HostCall) { call.SetI32(0, 70) }).
		Params(wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32).Results(wago.ValI32)
	imports.HostFunc("env", "splice", func(call wago.HostCall) { call.SetI32(0, emscriptenErrnoNosys) }).
		Params(wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32).Results(wago.ValI32)
	for _, name := range []string{"__sys_fstatat64", "__sys_openat", "__sys_prlimit64"} {
		imports.HostFunc("env", name, func(call wago.HostCall) { call.SetI32(0, emscriptenErrnoNosys) }).
			Params(wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32).Results(wago.ValI32)
	}
	imports.HostFunc("env", "__sys_fadvise64_64", func(call wago.HostCall) {
		call.SetI32(0, 0)
	}).Params(wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32).Results(wago.ValI32)
	for _, name := range []string{"__sys_fchdir", "__sys_chdir"} {
		imports.HostFunc("env", name, func(int32) int32 { return emscriptenErrnoNosys })
	}
	imports.HostFunc("env", "emscripten_get_heap_max", func(caller wago.Caller, call wago.HostCall) {
		call.SetI32(0, int32(len(caller.Memory())))
	}).Results(wago.ValI32)
	imports.HostFunc("env", "__sys_ugetrlimit", func(int32, int32) int32 { return emscriptenErrnoNosys })
	imports.HostFunc("env", "__sys_getdents64", func(call wago.HostCall) { call.SetI32(0, emscriptenErrnoNosys) }).
		Params(wago.ValI32, wago.ValI32, wago.ValI32).Results(wago.ValI32)
	imports.HostFunc("env", "time", func(int32) int32 { return 1 })
	imports.HostFunc("env", "clock", func(call wago.HostCall) { call.SetI32(0, 0) }).Results(wago.ValI32)
	imports.HostFunc("env", "difftime", func(call wago.HostCall) {
		call.SetF64(0, float64(call.I32(0)-call.I32(1)))
	}).Params(wago.ValI32, wago.ValI32).Results(wago.ValF64)
	imports.HostFunc("env", "localtime_r", func(caller wago.Caller, call wago.HostCall) {
		ptr := uint32(call.I32(1))
		mem := caller.Memory()
		if !validEmscriptenRange(mem, ptr, 44) {
			panic(wago.HostTrap{Err: fmt.Errorf("Emscripten localtime_r out of bounds")})
		}
		clear(mem[ptr : ptr+44])
		call.SetI32(0, int32(ptr))
	}).Params(wago.ValI32, wago.ValI32).Results(wago.ValI32)
	imports.HostFunc("env", "strftime", func(call wago.HostCall) { call.SetI32(0, 0) }).
		Params(wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32).Results(wago.ValI32)
	imports.HostFunc("env", "gettimeofday", func(caller wago.Caller, call wago.HostCall) {
		ptr := uint32(call.I32(0))
		mem := caller.Memory()
		if ptr != 0 {
			if !validEmscriptenRange(mem, ptr, 8) {
				panic(wago.HostTrap{Err: fmt.Errorf("Emscripten gettimeofday out of bounds")})
			}
			clear(mem[ptr : ptr+8])
		}
		call.SetI32(0, 0)
	}).Params(wago.ValI32, wago.ValI32).Results(wago.ValI32)
}

func validEmscriptenRange(mem []byte, offset, size uint32) bool {
	return uint64(offset)+uint64(size) <= uint64(len(mem))
}

func invokeWagoEmscriptenMain(in *wago.Instance, export string, args []string) ([]uint64, error) {
	if _, err := in.Invoke("__wasm_call_ctors"); err != nil {
		return nil, fmt.Errorf("constructors: %w", err)
	}
	ptrs := make([]uint32, len(args))
	mem := in.Memory().UnsafeBytes()
	for i, arg := range args {
		allocated, allocErr := in.Invoke("stackAlloc", wago.I32(int32(len(arg)+1)))
		if allocErr != nil || len(allocated) != 1 {
			return nil, fmt.Errorf("stackAlloc argument %d: results=%v error=%w", i, allocated, allocErr)
		}
		ptrs[i] = uint32(allocated[0])
		if !validEmscriptenRange(mem, ptrs[i], uint32(len(arg)+1)) {
			return nil, fmt.Errorf("argument %d allocation out of bounds", i)
		}
		copy(mem[ptrs[i]:], arg)
		mem[ptrs[i]+uint32(len(arg))] = 0
	}
	allocated, err := in.Invoke("stackAlloc", wago.I32(int32((len(args)+1)*4)))
	if err != nil || len(allocated) != 1 {
		return nil, fmt.Errorf("stackAlloc argv: results=%v error=%w", allocated, err)
	}
	argv := uint32(allocated[0])
	if !validEmscriptenRange(mem, argv, uint32((len(args)+1)*4)) {
		return nil, fmt.Errorf("argv allocation out of bounds")
	}
	for i, ptr := range ptrs {
		binary.LittleEndian.PutUint32(mem[argv+uint32(i*4):], ptr)
	}
	binary.LittleEndian.PutUint32(mem[argv+uint32(len(args)*4):], 0)
	return in.Invoke(export, wago.I32(int32(len(args))), wago.I32(int32(argv)))
}

func instantiateWazeroEmscriptenHost(ctx context.Context, r wazero.Runtime) error {
	b := r.NewHostModuleBuilder("env")
	b.NewFunctionBuilder().WithFunc(func(ctx context.Context, module api.Module, code uint32) {
		_ = module.CloseWithExitCode(ctx, code)
	}).Export("exit")
	b.NewFunctionBuilder().WithFunc(func() { panic(fmt.Errorf("Emscripten abort")) }).Export("abort")
	b.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32, uint32) { panic(fmt.Errorf("Emscripten assertion failed")) }).Export("__assert_fail")
	b.NewFunctionBuilder().WithFunc(func(uint32) uint32 { return 0 }).Export("emscripten_resize_heap")
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, module api.Module, dst, src, n uint32) uint32 {
		mem, ok := module.Memory().Read(0, module.Memory().Size())
		if !ok || !validEmscriptenRange(mem, dst, n) || !validEmscriptenRange(mem, src, n) {
			panic(fmt.Errorf("Emscripten memcpy out of bounds"))
		}
		copy(mem[dst:dst+n], mem[src:src+n])
		return dst
	}).Export("emscripten_memcpy_big")
	b.NewFunctionBuilder().WithFunc(func(uint32) {}).Export("setTempRet0")
	b.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32) int32 { return emscriptenErrnoNosys }).Export("__sys_open")
	b.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32) int32 { return emscriptenErrnoNosys }).Export("__sys_fcntl64")
	b.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32) int32 { return emscriptenErrnoNosys }).Export("__sys_ioctl")
	b.NewFunctionBuilder().WithFunc(func(uint32, uint32) int32 { return emscriptenErrnoNosys }).Export("__sys_chmod")
	b.NewFunctionBuilder().WithFunc(func(uint32) int32 { return emscriptenErrnoNosys }).Export("__sys_umask")
	b.NewFunctionBuilder().WithFunc(func(uint32, uint32) int32 { return emscriptenErrnoNosys }).Export("__sys_fchmod")
	for _, name := range []string{"__sys_lstat64", "__sys_stat64", "__sys_rename", "__clock_gettime"} {
		b.NewFunctionBuilder().WithFunc(func(uint32, uint32) int32 { return emscriptenErrnoNosys }).Export(name)
	}
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, module api.Module, fd, buf uint32) int32 {
		mem, ok := module.Memory().Read(0, module.Memory().Size())
		if !ok {
			return emscriptenErrnoNosys
		}
		return emscriptenFstat64(mem, fd, buf)
	}).Export("__sys_fstat64")
	for _, name := range []string{"__sys_readlink", "__sys_fchown32", "__sys_chown32"} {
		b.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32) int32 { return emscriptenErrnoNosys }).Export(name)
	}
	b.NewFunctionBuilder().WithFunc(func(uint32) int32 { return emscriptenErrnoNosys }).Export("__sys_unlink")
	b.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32, uint32, uint32) uint32 { return 70 }).Export("emscripten_fd_seek")
	b.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32, uint32, uint32, uint32) int32 { return emscriptenErrnoNosys }).Export("splice")
	for _, name := range []string{"__sys_fstatat64", "__sys_openat", "__sys_prlimit64"} {
		b.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32, uint32) int32 { return emscriptenErrnoNosys }).Export(name)
	}
	b.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32, uint32, uint32, uint32, uint32) uint32 { return 0 }).Export("__sys_fadvise64_64")
	for _, name := range []string{"__sys_fchdir", "__sys_chdir"} {
		b.NewFunctionBuilder().WithFunc(func(uint32) int32 { return emscriptenErrnoNosys }).Export(name)
	}
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, module api.Module) uint32 { return module.Memory().Size() }).Export("emscripten_get_heap_max")
	b.NewFunctionBuilder().WithFunc(func(uint32, uint32) int32 { return emscriptenErrnoNosys }).Export("__sys_ugetrlimit")
	b.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32) int32 { return emscriptenErrnoNosys }).Export("__sys_getdents64")
	b.NewFunctionBuilder().WithFunc(func(uint32) uint32 { return 1 }).Export("time")
	b.NewFunctionBuilder().WithFunc(func() uint32 { return 0 }).Export("clock")
	b.NewFunctionBuilder().WithFunc(func(a, b uint32) float64 { return float64(int32(a) - int32(b)) }).Export("difftime")
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, module api.Module, _ uint32, ptr uint32) uint32 {
		if !module.Memory().Write(ptr, make([]byte, 44)) {
			panic(fmt.Errorf("Emscripten localtime_r out of bounds"))
		}
		return ptr
	}).Export("localtime_r")
	b.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32, uint32) uint32 { return 0 }).Export("strftime")
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, module api.Module, ptr, _ uint32) uint32 {
		if ptr != 0 && !module.Memory().Write(ptr, make([]byte, 8)) {
			panic(fmt.Errorf("Emscripten gettimeofday out of bounds"))
		}
		return 0
	}).Export("gettimeofday")
	_, err := b.Instantiate(ctx)
	return err
}

func emscriptenFstat64(mem []byte, fd, buf uint32) int32 {
	if fd > 2 || !validEmscriptenRange(mem, buf, 88) {
		return emscriptenErrnoNosys
	}
	clear(mem[buf : buf+88])
	binary.LittleEndian.PutUint32(mem[buf+8:], fd+1)         // inode
	binary.LittleEndian.PutUint32(mem[buf+12:], 0x2000|0666) // character device and permissions
	binary.LittleEndian.PutUint32(mem[buf+16:], 1)           // link count
	binary.LittleEndian.PutUint32(mem[buf+48:], 4096)        // block size
	binary.LittleEndian.PutUint32(mem[buf+80:], fd+1)        // repeated inode
	return 0
}

func invokeWazeroEmscriptenMain(ctx context.Context, module api.Module, main api.Function, args []string) ([]uint64, error) {
	ctors := module.ExportedFunction("__wasm_call_ctors")
	stackAlloc := module.ExportedFunction("stackAlloc")
	if ctors == nil || stackAlloc == nil {
		return nil, fmt.Errorf("missing Emscripten lifecycle export")
	}
	if _, err := ctors.Call(ctx); err != nil {
		return nil, fmt.Errorf("constructors: %w", err)
	}
	ptrs := make([]uint32, len(args))
	for i, arg := range args {
		allocated, allocErr := stackAlloc.Call(ctx, uint64(len(arg)+1))
		if allocErr != nil || len(allocated) != 1 {
			return nil, fmt.Errorf("stackAlloc argument %d: results=%v error=%w", i, allocated, allocErr)
		}
		ptrs[i] = uint32(allocated[0])
		if !module.Memory().Write(ptrs[i], append([]byte(arg), 0)) {
			return nil, fmt.Errorf("argument %d allocation out of bounds", i)
		}
	}
	allocated, err := stackAlloc.Call(ctx, uint64((len(args)+1)*4))
	if err != nil || len(allocated) != 1 {
		return nil, fmt.Errorf("stackAlloc argv: results=%v error=%w", allocated, err)
	}
	argv := uint32(allocated[0])
	argvBytes := make([]byte, (len(args)+1)*4)
	for i, ptr := range ptrs {
		binary.LittleEndian.PutUint32(argvBytes[i*4:], ptr)
	}
	if !module.Memory().Write(argv, argvBytes) {
		return nil, fmt.Errorf("argv allocation out of bounds")
	}
	return main.Call(ctx, uint64(len(args)), uint64(argv))
}
