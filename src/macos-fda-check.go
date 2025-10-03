package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// checkFullDiskAccess attempts to deterministically detect whether the process
// has macOS Full Disk Access (FDA). It performs non-destructive reads on a
// set of paths that are typically protected by FDA. Returns nil if FDA appears
// granted, or an error describing the likely failure and a suggested action.
func checkFullDiskAccess() error {
	// Paths commonly protected by Full Disk Access. Keep this list conservative:
	testPaths := []string{
		// Mail
		"/Users/Shared/Library/Mail", // fallback for some setups
		"/Library/Mail",              // system-wide mail areas
		// Messages/chat
		filepath.Join(os.Getenv("HOME"), "Library/Messages"),
		// Safari/Keychain-like caches
		filepath.Join(os.Getenv("HOME"), "Library/Safari"),
		// Time Machine local dbs and backups (protected)
		"/var/db/backupd",              // Time Machine-related DB
		"/private/var/db",              // historically protected
		"/System/Library/CoreServices", // system area that can indicate broader restrictions
	}

	// Helper to attempt a safe read on a path.
	tryRead := func(p string) (string, error) {
		// Resolve symlink to its target to avoid false negatives on symlinked locations
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			p = resolved
		}
		info, err := os.Stat(p)
		if err != nil {
			// Distinguish NotExist vs permission errors
			if errors.Is(err, os.ErrNotExist) {
				return "not-exist", err
			}
			// on macOS permission errors often come back as EPERM or EACCES
			if pe, ok := err.(*os.PathError); ok {
				if errno, ok := pe.Err.(syscall.Errno); ok {
					if errno == syscall.EACCES || errno == syscall.EPERM {
						return "permission-denied", err
					}
				}
			}
			return "other-stat-error", err
		}

		// If it's a directory, try to open and read a small directory entry (non-destructive)
		if info.IsDir() {
			f, err := os.Open(p)
			if err != nil {
				if errors.Is(err, os.ErrPermission) {
					return "permission-denied", err
				}
				// os.Open may return *os.PathError wrapping errno
				if pe, ok := err.(*os.PathError); ok {
					if errno, ok := pe.Err.(syscall.Errno); ok {
						if errno == syscall.EACCES || errno == syscall.EPERM {
							return "permission-denied", err
						}
					}
				}
				return "other-open-error", err
			}
			defer f.Close()

			// Attempt a tiny read of directory contents
			_, err = f.Readdirnames(1)
			if err != nil && err != io.EOF {
				// treat permission-like errors specially
				if pe, ok := err.(*os.PathError); ok {
					if errno, ok := pe.Err.(syscall.Errno); ok {
						if errno == syscall.EACCES || errno == syscall.EPERM {
							return "permission-denied", err
						}
					}
				}
				return "other-readdir-error", err
			}
			return "ok", nil
		}

		// If it's a file, attempt a small, non-destructive open/read
		f, err := os.Open(p)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				return "permission-denied", err
			}
			if pe, ok := err.(*os.PathError); ok {
				if errno, ok := pe.Err.(syscall.Errno); ok {
					if errno == syscall.EACCES || errno == syscall.EPERM {
						return "permission-denied", err
					}
				}
			}
			return "other-open-error", err
		}
		defer f.Close()

		buf := make([]byte, 1)
		_, err = f.Read(buf)
		if err != nil && err != io.EOF {
			if pe, ok := err.(*os.PathError); ok {
				if errno, ok := pe.Err.(syscall.Errno); ok {
					if errno == syscall.EACCES || errno == syscall.EPERM {
						return "permission-denied", err
					}
				}
			}
			return "other-read-error", err
		}
		return "ok", nil
	}

	var permFailures []string
	for _, p := range testPaths {
		// Skip empty HOME-based entries if HOME not set
		if p == "" || (p[:1] == "." && os.Getenv("HOME") == "") {
			continue
		}
		status, err := tryRead(p)
		if status == "ok" {
			// If any protected path is accessible, that's positive signal; keep checking
			continue
		}
		// Only track permission denials
		if status == "permission-denied" {
			permFailures = append(permFailures, fmt.Sprintf("%s: %v", p, err))
		}
	}

	// Decision logic:
	// - Only fail if there are explicit permission denials
	// - Missing paths or other errors don't indicate lack of FDA
	if len(permFailures) > 0 {
		return fmt.Errorf("full disk access appears not granted: permission denied reading protected locations (%v). On macOS give the app Full Disk Access in System Settings → Privacy & Security → Full Disk Access", permFailures)
	}

	// No permission denials found - assume FDA is granted
	return nil
}
