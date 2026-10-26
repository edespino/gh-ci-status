// SPDX-License-Identifier: Apache-2.0

//go:build windows

package main

import "syscall"

// detachedProcess is DETACHED_PROCESS: no console, so the background server
// outlives the window it was started from.
const detachedProcess = 0x00000008

// detached starts the background server without a console, in its own
// process group, so it ignores the console's Ctrl-C.
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess}
}
