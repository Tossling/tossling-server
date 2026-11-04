//go:build linux

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// dropRoot runs the server and its commands as TOSSLING_RUN_AS when started as root, as the Docker image is. It
// first hands TOSSLING_DATA to that user, so a data directory that an older image wrote as root keeps working, and
// so does `docker compose exec tossling-server tossling-server …`, which comes in as root too.
func dropRoot() error {
	spec := os.Getenv("TOSSLING_RUN_AS")
	if spec == "" || os.Geteuid() != 0 {
		return nil
	}
	uid, gid, err := parseRunAs(spec)
	if err != nil {
		return err
	}
	if dir := os.Getenv("TOSSLING_DATA"); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		err := filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Lchown(path, uid, gid)
		})
		if err != nil {
			return fmt.Errorf("cannot hand %s to %d:%d: %w", dir, uid, gid, err)
		}
	}
	if err := syscall.Setgroups(nil); err != nil {
		return err
	}
	if err := syscall.Setgid(gid); err != nil {
		return err
	}
	return syscall.Setuid(uid)
}
