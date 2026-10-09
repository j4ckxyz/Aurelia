package main

import "golang.org/x/sys/unix"

// auditArch names this kind of machine to the filter of system calls.
const auditArch = unix.AUDIT_ARCH_X86_64
