package main

import "testing"

func TestRunAs(t *testing.T) {
	for spec, want := range map[string][2]int{"10001": {10001, 10001}, "10001:10002": {10001, 10002}} {
		uid, gid, err := parseRunAs(spec)
		if err != nil || uid != want[0] || gid != want[1] {
			t.Fatalf("%q: %d:%d %v", spec, uid, gid, err)
		}
	}
	for _, spec := range []string{"0", "0:0", "root", "10001:", "10001:x", "-5", ""} {
		if _, _, err := parseRunAs(spec); err == nil {
			t.Fatalf("%q is accepted", spec)
		}
	}
}
