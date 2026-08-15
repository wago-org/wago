//go:build linux

// Package watchsupervisor runs a watched runtime below a provider-free Linux
// subreaper so every fork remains attributable to that runtime.
package watchsupervisor

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const markerEnvironment = "WAGO_WATCH_SUPERVISOR"
const guestExecutableEnvironment = "WAGO_WATCH_GUEST_EXECUTABLE"
const parentPIDEnvironment = "WAGO_WATCH_SUPERVISOR_PARENT"
const parentLifetimeFDEnvironment = "WAGO_WATCH_PARENT_LIFETIME_FD"
const guardianStartupFDEnvironment = "WAGO_WATCH_GUARDIAN_STARTUP_FD"
const signalRelayReadyFDEnvironment = "WAGO_WATCH_SIGNAL_RELAY_READY_FD"
const signalRelayParentStartEnvironment = "WAGO_WATCH_SIGNAL_RELAY_PARENT_START"
const guardianRole = "guardian"
const workerRole = "worker"
const probeRole = "probe"
const signalRelayRole = "signal-relay"
const probeResponse = "wago-watch-supervisor-v1"
const maxDescendants = 4096
const maxThreads = 4096
const stopGrace = time.Second
const cleanupGrace = 500 * time.Millisecond
const probeTimeout = 2 * time.Second

type processInfo struct {
	pid, parent int
	state       byte
	started     uint64
}

// ChildLifetime holds the parent side of a pipe inherited by a child process.
// The child observes EOF only when this whole process exits or Close is called.
type ChildLifetime struct {
	read  *os.File
	write *os.File
}

// ChildIdentity identifies a process without trusting a reusable PID alone.
type ChildIdentity struct {
	PID     int
	Started uint64
}

// GuardianStartup carries the worker identity from a guardian to its parent.
type GuardianStartup struct {
	read  *os.File
	write *os.File
}

// SignalRelay joins a guest process group and relays its terminal interrupts to
// the watch process that started it.
type SignalRelay struct {
	command  *exec.Cmd
	lifetime *ChildLifetime
}

// StartSignalRelay starts a provider-free helper in group. The helper is ready
// to relay SIGINT and SIGQUIT before this function returns.
func StartSignalRelay(executable string, arguments, environment []string, group int) (*SignalRelay, error) {
	if executable == "" || group <= 0 {
		return nil, errors.New("invalid watch signal relay")
	}
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer readyRead.Close()
	base := environment
	if base == nil {
		base = os.Environ()
	}
	parent, ok := processByPID(os.Getpid())
	if !ok {
		_ = readyWrite.Close()
		return nil, errors.New("inspect watch signal relay parent")
	}
	command := exec.Command(executable, arguments...)
	command.Env = append(withoutInternalEnvironment(base),
		markerEnvironment+"="+signalRelayRole,
		parentPIDEnvironment+"="+strconv.Itoa(os.Getpid()),
		signalRelayReadyFDEnvironment+"=3",
		signalRelayParentStartEnvironment+"="+strconv.FormatUint(parent.started, 10),
	)
	command.ExtraFiles = []*os.File{readyWrite}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: group}
	lifetime, err := BindChild(command)
	if err != nil {
		_ = readyWrite.Close()
		return nil, err
	}
	relay := &SignalRelay{command: command, lifetime: lifetime}
	if err := command.Start(); err != nil {
		_ = readyWrite.Close()
		_ = lifetime.Close()
		return nil, err
	}
	lifetime.Started()
	if err := readyWrite.Close(); err != nil {
		_ = relay.Close()
		return nil, err
	}
	if err := readyRead.SetReadDeadline(time.Now().Add(probeTimeout)); err != nil {
		_ = relay.Close()
		return nil, err
	}
	var ready [1]byte
	if _, err := io.ReadFull(readyRead, ready[:]); err != nil || ready[0] != 1 {
		_ = relay.Close()
		if err == nil {
			err = errors.New("invalid watch signal relay response")
		}
		return nil, fmt.Errorf("start watch signal relay: %w", err)
	}
	return relay, nil
}

// Close stops and reaps the signal relay.
func (relay *SignalRelay) Close() error {
	if relay == nil {
		return nil
	}
	lifetimeErr := relay.lifetime.Close()
	relay.lifetime = nil
	if relay.command == nil || relay.command.Process == nil {
		return lifetimeErr
	}
	killErr := relay.command.Process.Kill()
	if errors.Is(killErr, os.ErrProcessDone) {
		killErr = nil
	}
	_ = relay.command.Wait()
	relay.command = nil
	return errors.Join(lifetimeErr, killErr)
}

// BindChild adds a process-lifetime pipe to command.
func BindChild(command *exec.Cmd) (*ChildLifetime, error) {
	read, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	fd := 3 + len(command.ExtraFiles)
	command.ExtraFiles = append(command.ExtraFiles, read)
	command.Env = replaceEnvironmentField(command.Environ(), parentLifetimeFDEnvironment, strconv.Itoa(fd))
	return &ChildLifetime{read: read, write: write}, nil
}

// BindGuardianStartup adds a pipe that reports the guardian's worker identity.
func BindGuardianStartup(command *exec.Cmd) (*GuardianStartup, error) {
	if !hasEnvironmentValue(command.Environ(), markerEnvironment, guardianRole) {
		return nil, nil
	}
	read, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	fd := 3 + len(command.ExtraFiles)
	command.ExtraFiles = append(command.ExtraFiles, write)
	command.Env = replaceEnvironmentField(command.Environ(), guardianStartupFDEnvironment, strconv.Itoa(fd))
	return &GuardianStartup{read: read, write: write}, nil
}

func hasEnvironmentValue(environment []string, name, value string) bool {
	want := name + "=" + value
	return slices.Contains(environment, want)
}

// Started releases the parent's copy of the inherited write side.
func (startup *GuardianStartup) Started() {
	if startup != nil && startup.write != nil {
		_ = startup.write.Close()
		startup.write = nil
	}
}

// Wait returns the worker identity reported by the worker itself.
func (startup *GuardianStartup) Wait() (ChildIdentity, error) {
	if startup == nil || startup.read == nil {
		return ChildIdentity{}, errors.New("missing watch guardian startup pipe")
	}
	if err := startup.read.SetReadDeadline(time.Now().Add(probeTimeout)); err != nil {
		return ChildIdentity{}, err
	}
	var data [16]byte
	if _, err := io.ReadFull(startup.read, data[:]); err != nil {
		return ChildIdentity{}, fmt.Errorf("read watch guardian startup: %w", err)
	}
	pid := binary.LittleEndian.Uint64(data[:8])
	started := binary.LittleEndian.Uint64(data[8:])
	if pid == 0 || pid > uint64(^uint(0)>>1) || started == 0 {
		return ChildIdentity{}, errors.New("invalid watch guardian worker identity")
	}
	return ChildIdentity{PID: int(pid), Started: started}, nil
}

// Close releases both parent-side startup pipe descriptors.
func (startup *GuardianStartup) Close() error {
	if startup == nil {
		return nil
	}
	var readErr, writeErr error
	if startup.read != nil {
		readErr = startup.read.Close()
		startup.read = nil
	}
	if startup.write != nil {
		writeErr = startup.write.Close()
		startup.write = nil
	}
	return errors.Join(readErr, writeErr)
}

// Started releases the parent's copy of the inherited read side.
func (lifetime *ChildLifetime) Started() {
	if lifetime != nil && lifetime.read != nil {
		_ = lifetime.read.Close()
		lifetime.read = nil
	}
}

// Close releases both parent-side pipe descriptors.
func (lifetime *ChildLifetime) Close() error {
	if lifetime == nil {
		return nil
	}
	var readErr, writeErr error
	if lifetime.read != nil {
		readErr = lifetime.read.Close()
		lifetime.read = nil
	}
	if lifetime.write != nil {
		writeErr = lifetime.write.Close()
		lifetime.write = nil
	}
	return errors.Join(readErr, writeErr)
}

func replaceEnvironmentField(environment []string, name, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

// Environment marks a provider-free executable as a supervisor for guest.
func Environment(base []string, guest string) []string {
	if base == nil {
		base = os.Environ()
	}
	result := withoutInternalEnvironment(base)
	return append(result, markerEnvironment+"="+guardianRole, guestExecutableEnvironment+"="+guest)
}

// Probe verifies that manager understands this supervisor protocol.
func Probe(manager string) error {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, manager, "--help")
	command.Env = append(withoutInternalEnvironment(os.Environ()), markerEnvironment+"="+probeRole)
	var output probeOutput
	command.Stdout = &output
	command.Stderr = io.Discard
	err := command.Run()
	if ctx.Err() != nil {
		return errors.New("manager watch supervisor probe timed out")
	}
	if err != nil {
		return fmt.Errorf("manager watch supervisor probe failed: %w", err)
	}
	if output.overflow || strings.TrimSpace(output.String()) != probeResponse {
		return errors.New("manager does not support watch supervisor protocol v1")
	}
	return nil
}

type probeOutput struct {
	data     [64]byte
	length   int
	overflow bool
}

func (output *probeOutput) Write(data []byte) (int, error) {
	written := copy(output.data[output.length:], data)
	output.length += written
	output.overflow = output.overflow || written != len(data)
	return len(data), nil
}

func (output *probeOutput) String() string { return string(output.data[:output.length]) }

// Enter starts the marked guest and exits. It returns for normal invocations.
func Enter() {
	role := os.Getenv(markerEnvironment)
	if role == "" {
		return
	}
	if role == probeRole {
		_, _ = fmt.Fprintln(os.Stdout, probeResponse)
		os.Exit(0)
	}
	if role == signalRelayRole {
		if err := runSignalRelay(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "wago: watch signal relay: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if role == workerRole {
		if err := reportGuardianStartup(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "wago: watch supervisor: %v\n", err)
			os.Exit(1)
		}
	}
	guest := os.Getenv(guestExecutableEnvironment)
	if guest == "" {
		_, _ = fmt.Fprintln(os.Stderr, "wago: watch supervisor: missing guest executable")
		os.Exit(1)
	}
	command, err := supervisorCommand(role, guest)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "wago: watch supervisor: %v\n", err)
		os.Exit(1)
	}
	var startup *os.File
	if role == guardianRole {
		startup, err = forwardGuardianStartup(command)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "wago: watch supervisor: %v\n", err)
			os.Exit(1)
		}
		if startup != nil {
			defer startup.Close()
		}
	}
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	code, err := run(command, func() {
		if startup != nil {
			_ = startup.Close()
			startup = nil
		}
	})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "wago: watch supervisor: %v\n", err)
		code = 1
	}
	os.Exit(code)
}

func forwardGuardianStartup(command *exec.Cmd) (*os.File, error) {
	value := os.Getenv(guardianStartupFDEnvironment)
	if value == "" {
		return nil, nil
	}
	fd, err := strconv.Atoi(value)
	if err != nil || fd < 3 {
		return nil, errors.New("invalid watch guardian startup descriptor")
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
		return nil, fmt.Errorf("inspect watch guardian startup descriptor: %w", err)
	}
	file := os.NewFile(uintptr(fd), "wago-watch-guardian-startup")
	if file == nil {
		return nil, errors.New("open watch guardian startup descriptor")
	}
	unix.CloseOnExec(fd)
	childFD := 3 + len(command.ExtraFiles)
	command.ExtraFiles = append(command.ExtraFiles, file)
	command.Env = replaceEnvironmentField(command.Environ(), guardianStartupFDEnvironment, strconv.Itoa(childFD))
	return file, nil
}

func reportGuardianStartup() error {
	value := os.Getenv(guardianStartupFDEnvironment)
	if value == "" {
		return nil
	}
	fd, err := strconv.Atoi(value)
	if err != nil || fd < 3 {
		return errors.New("invalid watch guardian startup descriptor")
	}
	file := os.NewFile(uintptr(fd), "wago-watch-guardian-startup")
	if file == nil {
		return errors.New("open watch guardian startup descriptor")
	}
	unix.CloseOnExec(fd)
	defer file.Close()
	process, ok := processByPID(os.Getpid())
	if !ok {
		return errors.New("inspect watch guardian worker")
	}
	var data [16]byte
	binary.LittleEndian.PutUint64(data[:8], uint64(process.pid))
	binary.LittleEndian.PutUint64(data[8:], process.started)
	if _, err := file.Write(data[:]); err != nil {
		return fmt.Errorf("report watch guardian worker: %w", err)
	}
	return nil
}

func supervisorCommand(role, guest string) (*exec.Cmd, error) {
	environment := withoutInternalEnvironment(os.Environ())
	switch role {
	case guardianRole:
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		command := exec.Command(executable, os.Args[1:]...)
		command.Env = append(environment,
			markerEnvironment+"="+workerRole,
			guestExecutableEnvironment+"="+guest,
			parentPIDEnvironment+"="+strconv.Itoa(os.Getpid()),
		)
		return command, nil
	case workerRole:
		expected, err := strconv.Atoi(os.Getenv(parentPIDEnvironment))
		if err != nil || expected <= 0 || syscall.Getppid() != expected {
			return nil, errors.New("guardian exited before worker startup")
		}
		command := exec.Command(guest, os.Args[1:]...)
		command.Env = environment
		return command, nil
	default:
		return nil, errors.New("invalid supervisor role")
	}
}

func withoutInternalEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		if strings.HasPrefix(entry, markerEnvironment+"=") ||
			strings.HasPrefix(entry, guestExecutableEnvironment+"=") ||
			strings.HasPrefix(entry, parentPIDEnvironment+"=") ||
			strings.HasPrefix(entry, parentLifetimeFDEnvironment+"=") ||
			strings.HasPrefix(entry, guardianStartupFDEnvironment+"=") ||
			strings.HasPrefix(entry, signalRelayReadyFDEnvironment+"=") ||
			strings.HasPrefix(entry, signalRelayParentStartEnvironment+"=") {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func runSignalRelay() error {
	parent, err := strconv.Atoi(os.Getenv(parentPIDEnvironment))
	if err != nil || parent <= 0 || syscall.Getppid() != parent {
		return errors.New("signal relay parent exited before startup")
	}
	parentStart, err := strconv.ParseUint(os.Getenv(signalRelayParentStartEnvironment), 10, 64)
	if err != nil || parentStart == 0 {
		return errors.New("invalid signal relay parent identity")
	}
	parentIdentity := processInfo{pid: parent, started: parentStart}
	if current, ok := processByPID(parent); !ok || current.started != parentStart {
		return errors.New("signal relay parent changed before startup")
	}
	readyFD, err := strconv.Atoi(os.Getenv(signalRelayReadyFDEnvironment))
	if err != nil || readyFD < 3 {
		return errors.New("invalid signal relay ready descriptor")
	}
	ready := os.NewFile(uintptr(readyFD), "wago-watch-signal-relay-ready")
	if ready == nil {
		return errors.New("open signal relay ready descriptor")
	}
	unix.CloseOnExec(readyFD)
	defer ready.Close()
	lifetime, parentExited, err := monitorParentLifetime()
	if err != nil {
		return err
	}
	if lifetime == nil {
		return errors.New("signal relay requires a parent lifetime descriptor")
	}
	defer lifetime.Close()
	events := make(chan os.Signal, 2)
	signal.Notify(events, os.Interrupt, syscall.SIGQUIT)
	defer signal.Stop(events)
	if _, err := ready.Write([]byte{1}); err != nil {
		return err
	}
	if err := ready.Close(); err != nil {
		return err
	}
	for {
		select {
		case <-parentExited:
			return nil
		case received := <-events:
			value, ok := received.(syscall.Signal)
			if !ok {
				continue
			}
			if err := signalProcessIdentity(parentIdentity, value); err != nil {
				if errors.Is(err, os.ErrProcessDone) {
					return nil
				}
				return err
			}
		}
	}
}

// Run supervises command and returns its process exit code.
func Run(command *exec.Cmd) (int, error) {
	return run(command, nil)
}

func run(command *exec.Cmd, started func()) (int, error) {
	parentLifetime, parentExited, err := monitorParentLifetime()
	if err != nil {
		return 1, err
	}
	if parentLifetime != nil {
		defer parentLifetime.Close()
	}
	if err := prepare(); err != nil {
		return 1, err
	}
	events := make(chan os.Signal, 8)
	signal.Notify(events, os.Interrupt, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGCONT, syscall.SIGCHLD)
	defer signal.Stop(events)
	childLifetime, err := BindChild(command)
	if err != nil {
		return 1, err
	}
	defer childLifetime.Close()
	select {
	case <-parentExited:
		return signalExitCode(syscall.SIGTERM), nil
	default:
	}
	if err := command.Start(); err != nil {
		return 1, err
	}
	childLifetime.Started()
	if started != nil {
		started()
	}
	directChild, ok := processByPID(command.Process.Pid)
	if !ok {
		_ = command.Process.Kill()
		_ = command.Wait()
		return 1, errors.New("inspect watch supervisor child")
	}
	defer command.Process.Release()
	states := make(chan syscall.WaitStatus, 8)
	exited := make(chan childExit, 1)
	go waitDirectChild(command.Process.Pid, states, exited)
	stopChild := func(received os.Signal) (int, error) {
		timer := time.NewTimer(stopGrace)
		select {
		case <-exited:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
			_ = signalProcessIdentity(directChild, syscall.SIGKILL)
			<-exited
		}
		if cleanupErr := cleanup(); cleanupErr != nil {
			return 1, cleanupErr
		}
		return signalExitCode(received), nil
	}
	for {
		select {
		case <-parentExited:
			return stopChild(syscall.SIGTERM)
		case result := <-exited:
			if cleanupErr := cleanup(); cleanupErr != nil {
				return 1, cleanupErr
			}
			return commandExitCode(result), result.err
		case status := <-states:
			if status.Stopped() {
				child, ok := processByPID(command.Process.Pid)
				if !ok || child.started != directChild.started || (child.state != 'T' && child.state != 't') {
					continue
				}
				if err := syscall.Kill(os.Getpid(), syscall.SIGSTOP); err != nil {
					_ = signalProcessIdentity(directChild, syscall.SIGKILL)
					result := <-exited
					return 1, errors.Join(err, result.err, cleanup())
				}
				if err := signalProcessIdentity(directChild, syscall.SIGCONT); err != nil && !errors.Is(err, os.ErrProcessDone) {
					_ = signalProcessIdentity(directChild, syscall.SIGKILL)
					result := <-exited
					return 1, errors.Join(err, result.err, cleanup())
				}
			}
		case received := <-events:
			switch received {
			case syscall.SIGCHLD:
				if err := reapAdopted(command.Process.Pid); err != nil {
					_ = signalProcessIdentity(directChild, syscall.SIGKILL)
					result := <-exited
					return 1, errors.Join(err, result.err, cleanup())
				}
				continue
			case syscall.SIGCONT:
				if err := signalProcessIdentity(directChild, syscall.SIGCONT); err != nil && !errors.Is(err, os.ErrProcessDone) {
					_ = signalProcessIdentity(directChild, syscall.SIGKILL)
					result := <-exited
					return 1, errors.Join(err, result.err, cleanup())
				}
				continue
			}
			return stopChild(received)
		}
	}
}

func monitorParentLifetime() (*os.File, <-chan struct{}, error) {
	value := os.Getenv(parentLifetimeFDEnvironment)
	if value == "" {
		return nil, nil, nil
	}
	fd, err := strconv.Atoi(value)
	if err != nil || fd < 3 {
		return nil, nil, errors.New("invalid watch parent lifetime descriptor")
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
		return nil, nil, fmt.Errorf("inspect watch parent lifetime descriptor: %w", err)
	}
	file := os.NewFile(uintptr(fd), "wago-watch-parent-lifetime")
	if file == nil {
		return nil, nil, errors.New("open watch parent lifetime descriptor")
	}
	unix.CloseOnExec(fd)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		var buffer [1]byte
		for {
			if _, err := file.Read(buffer[:]); err != nil {
				return
			}
		}
	}()
	return file, exited, nil
}

type childExit struct {
	status syscall.WaitStatus
	err    error
}

func signalProcessIdentity(process processInfo, value syscall.Signal) error {
	current, ok := processByPID(process.pid)
	if !ok || current.started != process.started {
		return os.ErrProcessDone
	}
	if err := syscall.Kill(process.pid, value); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return nil
}

func waitDirectChild(pid int, states chan<- syscall.WaitStatus, exited chan<- childExit) {
	for {
		var status syscall.WaitStatus
		_, err := syscall.Wait4(pid, &status, syscall.WUNTRACED|syscall.WCONTINUED, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			exited <- childExit{err: err}
			return
		}
		if status.Stopped() || status.Continued() {
			states <- status
			continue
		}
		exited <- childExit{status: status}
		return
	}
}

func prepare() error {
	children, err := os.Open("/proc/self/task/" + strconv.Itoa(os.Getpid()) + "/children")
	if err != nil {
		return fmt.Errorf("watch process tracking requires procfs child lists: %w", err)
	}
	if err := children.Close(); err != nil {
		return fmt.Errorf("watch process tracking requires procfs child lists: %w", err)
	}
	return unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
}

func reapAdopted(guestPID int) error {
	processes, scanErr := descendants(os.Getpid(), maxDescendants)
	for _, process := range processes {
		if process.parent != os.Getpid() || process.pid == guestPID {
			continue
		}
		current, ok := processByPID(process.pid)
		if !ok || current.started != process.started || current.parent != os.Getpid() {
			continue
		}
		var status syscall.WaitStatus
		_, _ = syscall.Wait4(process.pid, &status, syscall.WNOHANG, nil)
	}
	return scanErr
}

func cleanup() error {
	deadline := time.Now().Add(cleanupGrace)
	var scanErr error
	for {
		processes, err := descendants(os.Getpid(), maxDescendants)
		if err != nil && scanErr == nil {
			scanErr = err
		}
		for _, process := range processes {
			current, ok := processByPID(process.pid)
			if !ok || current.started != process.started {
				continue
			}
			if killErr := syscall.Kill(process.pid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
				scanErr = errors.Join(scanErr, killErr)
			}
		}
		reapAll()
		if len(processes) == 0 {
			return scanErr
		}
		if time.Now().After(deadline) {
			return errors.Join(scanErr, errors.New("watched process cleanup timed out"))
		}
		time.Sleep(time.Millisecond)
	}
}

func descendants(root, limit int) ([]processInfo, error) {
	rootProcess, ok := processByPID(root)
	if !ok {
		return nil, os.ErrProcessDone
	}
	queue := []processInfo{rootProcess}
	seen := map[int]bool{root: true}
	processes := make([]processInfo, 0)
	for len(queue) != 0 {
		parent := queue[0]
		queue = queue[1:]
		current, exists := processByPID(parent.pid)
		if !exists || current.started != parent.started {
			continue
		}
		tasks, err := processTasks(parent.pid, maxThreads)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return processes, err
		}
		for _, task := range tasks {
			file, openErr := os.Open("/proc/" + strconv.Itoa(parent.pid) + "/task/" + strconv.Itoa(task) + "/children")
			if errors.Is(openErr, os.ErrNotExist) {
				continue
			}
			if openErr != nil {
				return processes, openErr
			}
			scanner := bufio.NewScanner(file)
			scanner.Split(bufio.ScanWords)
			for scanner.Scan() {
				pid, parseErr := strconv.Atoi(scanner.Text())
				if parseErr != nil || seen[pid] {
					continue
				}
				process, childExists := processByPID(pid)
				if !childExists || process.parent != parent.pid {
					continue
				}
				if len(processes) >= limit {
					_ = file.Close()
					return processes, fmt.Errorf("watched process tree exceeds %d descendants", limit)
				}
				seen[pid] = true
				processes = append(processes, process)
				queue = append(queue, process)
			}
			scanErr := scanner.Err()
			closeErr := file.Close()
			if scanErr != nil {
				return processes, scanErr
			}
			if closeErr != nil {
				return processes, closeErr
			}
		}
	}
	return processes, nil
}

func processTasks(pid, limit int) ([]int, error) {
	directory, err := os.Open("/proc/" + strconv.Itoa(pid) + "/task")
	if err != nil {
		return nil, err
	}
	names, readErr := directory.Readdirnames(limit + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(names) > limit {
		return nil, fmt.Errorf("watched process exceeds %d threads", limit)
	}
	tasks := make([]int, 0, len(names))
	for _, name := range names {
		if task, parseErr := strconv.Atoi(name); parseErr == nil {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func processByPID(pid int) (processInfo, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return processInfo{}, false
	}
	close := strings.LastIndex(string(data), ") ")
	if close < 0 {
		return processInfo{}, false
	}
	fields := strings.Fields(string(data[close+2:]))
	if len(fields) <= 19 {
		return processInfo{}, false
	}
	parent, parentErr := strconv.Atoi(fields[1])
	started, startedErr := strconv.ParseUint(fields[19], 10, 64)
	if parentErr != nil || startedErr != nil {
		return processInfo{}, false
	}
	state := byte(0)
	if fields[0] != "" {
		state = fields[0][0]
	}
	return processInfo{pid: pid, parent: parent, state: state, started: started}, true
}

func reapAll() {
	for {
		var status syscall.WaitStatus
		pid, _ := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if pid <= 0 {
			return
		}
	}
}

func commandExitCode(result childExit) int {
	if result.err != nil {
		return 1
	}
	if result.status.Signaled() {
		return 128 + int(result.status.Signal())
	}
	return result.status.ExitStatus()
}

func signalExitCode(value os.Signal) int {
	if signalValue, ok := value.(syscall.Signal); ok {
		return 128 + int(signalValue)
	}
	return 1
}
