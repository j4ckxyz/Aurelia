//go:build linux && !amd64 && !arm64

package main

// auditArch is 0 where the filter of system calls is not written for the
// kind of machine: Landlock alone holds there.
const auditArch = 0
