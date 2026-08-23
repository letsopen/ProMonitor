package aggregator

import (
	"testing"
	"time"
)

func TestBucketKey(t *testing.T) {
	// 10:05:00 → 桶起点 10:00:00
	base := time.Date(2026, 8, 23, 10, 5, 0, 0, time.UTC).Unix()
	got := bucketKey(base)
	want := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC).Unix()
	if got != want {
		t.Fatalf("bucketKey(%d)=%d want %d", base, got, want)
	}
	// 10:00:00 整点
	got2 := bucketKey(time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC).Unix())
	if got2 != want {
		t.Fatalf("bucketKey(exact)=%d want %d", got2, want)
	}
	// 10:59:59 → 仍属 10:50 桶
	got3 := bucketKey(time.Date(2026, 8, 23, 10, 59, 59, 0, time.UTC).Unix())
	want3 := time.Date(2026, 8, 23, 10, 50, 0, 0, time.UTC).Unix()
	if got3 != want3 {
		t.Fatalf("bucketKey(10:59:59)=%d want %d", got3, want3)
	}
}
