package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// landlockPort is the kernel's landlock_net_port_attr.
type landlockPort struct {
	allowed uint64
	port    uint64
}

const landlockRuleNetPort = 2

// sandbox gives up, for good and for every thread, what the proxy does
// not need, with Landlock: any file but those named, which it may only
// read, and any outgoing TCP connection but to the ports named. Listening
// sockets opened before stay; none can be opened after. Whoever took the
// proxy over could then read nothing of the machine, and reach none of
// its other services. It needs no privilege, and returns what it did.
func sandbox(readable []string, connectPorts []string) (string, error) {
	version, _, errno := syscall.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return "", fmt.Errorf("this kernel has no Landlock: %w", errno)
	}
	// Every right over files this kernel knows, so that every one is
	// withheld.
	files := uint64(1<<13 - 1)
	if version >= 2 {
		files |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if version >= 3 {
		files |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	if version >= 5 {
		files |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	attr := unix.LandlockRulesetAttr{Access_fs: files}
	if version >= 4 {
		attr.Access_net = unix.LANDLOCK_ACCESS_NET_BIND_TCP | unix.LANDLOCK_ACCESS_NET_CONNECT_TCP
	}
	if version >= 6 {
		attr.Scoped = unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET | unix.LANDLOCK_SCOPE_SIGNAL
	}
	fd, _, errno := syscall.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return "", fmt.Errorf("landlock_create_ruleset: %w", errno)
	}
	defer unix.Close(int(fd))
	for _, path := range readable {
		// The file itself, where the name is a link to it.
		p, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			continue // not on this system
		}
		var st unix.Stat_t
		allowed := uint64(unix.LANDLOCK_ACCESS_FS_READ_FILE)
		if unix.Fstat(p, &st) == nil && st.Mode&unix.S_IFMT == unix.S_IFDIR {
			allowed |= unix.LANDLOCK_ACCESS_FS_READ_DIR
		}
		rule := unix.LandlockPathBeneathAttr{Allowed_access: allowed, Parent_fd: int32(p)}
		_, _, errno := syscall.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&rule)), 0, 0, 0)
		unix.Close(p)
		if errno != 0 {
			return "", fmt.Errorf("allowing %s: %w", path, errno)
		}
	}
	network := "any connection, which this kernel's Landlock does not limit"
	if version >= 4 {
		for _, port := range connectPorts {
			n, err := strconv.ParseUint(port, 10, 16)
			if err != nil {
				return "", fmt.Errorf("the port %q", port)
			}
			rule := landlockPort{allowed: unix.LANDLOCK_ACCESS_NET_CONNECT_TCP, port: n}
			if _, _, errno := syscall.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, landlockRuleNetPort, uintptr(unsafe.Pointer(&rule)), 0, 0, 0); errno != 0 {
				return "", fmt.Errorf("allowing port %s: %w", port, errno)
			}
		}
		network = "connections to ports " + strings.Join(connectPorts, ", ") + " only, and no new listening"
	}
	// On every thread of the process: Go runs on several.
	if _, _, errno := syscall.AllThreadsSyscall(unix.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0); errno == unix.ENOTSUP {
		// Go cannot do it on every thread of a program linked with C.
		return "", errors.New("the sandbox needs a build without cgo: CGO_ENABLED=0")
	} else if errno != 0 {
		return "", fmt.Errorf("no_new_privs: %w", errno)
	}
	// Landlock's rules are for TCP. Multipath TCP is another protocol to
	// the kernel, though it reaches the same services: so no socket but
	// of TCP and UDP may be made at all.
	sockets := "; any kind of socket, which this machine's kind has no filter for here"
	if err := onlyPlainSockets(); err == nil {
		sockets = "; sockets of TCP and UDP only"
	} else if !errors.Is(err, errNoFilter) {
		return "", err
	}
	if _, _, errno := syscall.AllThreadsSyscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); errno != 0 {
		return "", fmt.Errorf("landlock_restrict_self: %w", errno)
	}
	// Told only once it holds.
	if _, err := os.ReadFile("/proc/self/environ"); err == nil {
		return "", errors.New("the sandbox does not hold: a file it should refuse was read")
	}
	return "no files but the resolver's, " + network + sockets, nil
}

var errNoFilter = errors.New("no filter for this architecture")

// onlyPlainSockets has the kernel refuse, with seccomp, every socket that
// is not TCP or UDP over IPv4 or IPv6, and io_uring, which makes sockets
// without the system call the filter sees. It holds for every thread.
func onlyPlainSockets() error {
	if auditArch == 0 {
		return errNoFilter
	}
	const (
		load      = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
		equal     = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
		atLeast   = unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K
		ret       = unix.BPF_RET | unix.BPF_K
		allow     = 0x7fff0000 // SECCOMP_RET_ALLOW
		refuse    = 0x00050000 // SECCOMP_RET_ERRNO
		offNumber = 0
		offArch   = 4
		offArg0   = 16
		offArg2   = 32
	)
	// Each jump counts the instructions it skips: the three returns are
	// the last three.
	prog := []unix.SockFilter{
		{Code: load, K: offArch},
		{Code: equal, K: auditArch, Jt: 1, Jf: 0},
		{Code: ret, K: refuse | uint32(unix.EPERM)}, // a call of another kind of program: none is needed
		{Code: load, K: offNumber},
		{Code: atLeast, K: 0x40000000, Jt: 12, Jf: 0}, // the x32 calls, numbered apart
		{Code: equal, K: unix.SYS_IO_URING_SETUP, Jt: 11, Jf: 0},
		{Code: equal, K: unix.SYS_IO_URING_ENTER, Jt: 10, Jf: 0},
		{Code: equal, K: unix.SYS_IO_URING_REGISTER, Jt: 9, Jf: 0},
		{Code: equal, K: unix.SYS_SOCKET, Jt: 0, Jf: 7}, // anything else is allowed
		{Code: load, K: offArg0},                        // the family
		{Code: equal, K: unix.AF_INET, Jt: 1, Jf: 0},
		{Code: equal, K: unix.AF_INET6, Jt: 0, Jf: 5},
		{Code: load, K: offArg2}, // the protocol
		{Code: equal, K: 0, Jt: 2, Jf: 0},
		{Code: equal, K: unix.IPPROTO_TCP, Jt: 1, Jf: 0},
		{Code: equal, K: unix.IPPROTO_UDP, Jt: 0, Jf: 1},
		{Code: ret, K: allow},
		{Code: ret, K: refuse | uint32(unix.EPROTONOSUPPORT)},
	}
	fprog := unix.SockFprog{Len: uint16(len(prog)), Filter: &prog[0]}
	const setModeFilter, everyThread = 1, 1 // SECCOMP_SET_MODE_FILTER, SECCOMP_FILTER_FLAG_TSYNC
	if _, _, errno := syscall.Syscall(unix.SYS_SECCOMP, setModeFilter, everyThread, uintptr(unsafe.Pointer(&fprog))); errno != 0 {
		return fmt.Errorf("seccomp: %w", errno)
	}
	return nil
}
