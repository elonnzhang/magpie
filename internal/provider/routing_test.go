package provider

import (
	"testing"
	"time"
)

// An account's allowance is as used as its fullest window and new again
// when the last of them resets.
func TestAllowanceOf(t *testing.T) {
	now := time.Now()
	week := now.Add(3 * 24 * time.Hour)
	a := allowanceOf([]QuotaWindow{
		{Name: "5 hours", Used: 80, ResetSecs: 2 * 3600},
		{Name: "7 days", Used: 30, ResetsAt: &week},
	}, now)
	if a.Used != 80 || !a.Resets.Equal(week) {
		t.Fatalf("%+v", a)
	}
	a = allowanceOf([]QuotaWindow{{Name: "5 hours", Used: 10, ResetSecs: 3600}}, now)
	if !a.Resets.Equal(now.Add(time.Hour)) {
		t.Fatalf("from seconds: %+v", a)
	}
	if a = allowanceOf([]QuotaWindow{{Used: 10}}, now); !a.Resets.IsZero() {
		t.Fatalf("not known: %+v", a)
	}
}
