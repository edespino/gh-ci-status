// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package main

import "syscall"

// detached starts the background server in its own session, so it outlives
// the terminal and ignores the terminal's Ctrl-C.
func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
