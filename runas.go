package main

import (
	"fmt"
	"strconv"
	"strings"
)

// parseRunAs reads TOSSLING_RUN_AS: "uid" or "uid:gid", numeric, not root.
func parseRunAs(spec string) (uid, gid int, err error) {
	uidText, gidText, hasGID := strings.Cut(spec, ":")
	uid, err = strconv.Atoi(uidText)
	if err != nil || uid <= 0 {
		return 0, 0, fmt.Errorf("TOSSLING_RUN_AS=%q: the user must be a number above 0", spec)
	}
	gid = uid
	if hasGID {
		gid, err = strconv.Atoi(gidText)
		if err != nil || gid <= 0 {
			return 0, 0, fmt.Errorf("TOSSLING_RUN_AS=%q: the group must be a number above 0", spec)
		}
	}
	return uid, gid, nil
}
