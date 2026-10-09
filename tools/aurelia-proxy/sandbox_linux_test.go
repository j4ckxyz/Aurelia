package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The sandbox cannot be left, so a process of its own enters it: this
// test, run again with a mark, which then tries what should be refused.
func TestSandbox(t *testing.T) {
	if os.Getenv("AURELIA_PROXY_IN_SANDBOX") != "" {
		inSandbox()
		return
	}
	other, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	go func() {
		for {
			c, err := other.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestSandbox$")
	cmd.Env = append(os.Environ(), "AURELIA_PROXY_IN_SANDBOX="+other.Addr().String())
	out, err := cmd.CombinedOutput()
	got := string(out)
	t.Log(strings.TrimSpace(got))
	for _, why := range []string{"no Landlock", "without cgo"} {
		if strings.Contains(got, why) {
			if os.Getenv("AURELIA_REQUIRE_SANDBOX") != "" {
				t.Fatal("no sandbox here, and AURELIA_REQUIRE_SANDBOX asks for one")
			}
			t.Skip("no sandbox here")
		}
	}
	if err != nil {
		t.Fatalf("the sandboxed process failed: %v", err)
	}
	for _, want := range []string{
		"sandbox: no files",
		"a file of the system: refused",
		"a new file: refused",
		"the resolver's file: read",
		"another service of this machine: refused",
		"listening anew on 127.0.0.1:0: refused",
		"listening anew on 127.0.0.1:18443: refused",
		"listening anew on [::1]:18444: refused",
		"listening anew on 0.0.0.0:18445: refused",
		"a socket of Multipath TCP: refused",
		"a socket of another family: refused",
		"a socket of UDP, for the resolver: made",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("in the sandbox, want %q", want)
		}
	}
}

func inSandbox() {
	what, err := sandbox([]string{"/etc/hosts"}, []string{"443"})
	if err != nil {
		fmt.Println("sandbox failed:", err)
		os.Exit(0)
	}
	fmt.Println("sandbox:", what)
	say := func(what string, err error, ok string) {
		if err != nil {
			fmt.Printf("%s: refused\n", what)
		} else {
			fmt.Printf("%s: %s\n", what, ok)
		}
	}
	_, err = os.ReadFile("/etc/passwd")
	say("a file of the system", err, "read")
	err = os.WriteFile(os.TempDir()+"/aurelia-proxy-sandbox-test", []byte("x"), 0o600)
	say("a new file", err, "written")
	_, err = os.ReadFile("/etc/hosts")
	say("the resolver's file", err, "read")
	conn, err := net.Dial("tcp", os.Getenv("AURELIA_PROXY_IN_SANDBOX"))
	if conn != nil {
		conn.Close()
	}
	say("another service of this machine", err, "reached")
	for _, addr := range []string{"127.0.0.1:0", "127.0.0.1:18443", "[::1]:18444", "0.0.0.0:18445"} {
		ln, err := net.Listen("tcp", addr)
		if ln != nil {
			ln.Close()
		}
		say("listening anew on "+addr, err, "allowed")
	}
	// Multipath TCP, which Landlock's rules for TCP do not hold, and
	// which reaches the same services.
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, unix.IPPROTO_MPTCP)
	if err == nil {
		unix.Close(fd)
	}
	say("a socket of Multipath TCP", err, "made")
	fd, err = unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err == nil {
		unix.Close(fd)
	}
	say("a socket of another family", err, "made")
	// What the proxy does need still works.
	fd, err = unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err == nil {
		unix.Close(fd)
		fmt.Println("a socket of UDP, for the resolver: made")
	}
	os.Exit(0)
}
