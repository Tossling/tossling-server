package main

import (
	"fmt"
	"testing"
	"time"
)

func TestTheLimiterForgetsOldAddresses(t *testing.T) {
	l := &loginLimiter{fails: map[string]*loginFails{}}
	old := time.Now().Add(-2 * loginLockout)
	for i := 0; i < loginLimiterKeep; i++ {
		l.fails[fmt.Sprintf("10.0.%d.%d", i/256, i%256)] = &loginFails{count: 1, first: old}
	}
	l.fails["10.9.9.9"] = &loginFails{count: loginAttempts, first: old, until: time.Now().Add(time.Minute)}
	l.fail("192.0.2.7")
	if len(l.fails) != 2 {
		t.Fatalf("%d addresses kept, want the locked one and the new one", len(l.fails))
	}
	if !l.blocked("10.9.9.9") {
		t.Fatal("a locked address was forgotten")
	}
}
