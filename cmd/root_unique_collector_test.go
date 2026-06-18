package cmd

import (
	"slices"
	"testing"
)

func TestUniqueIDCollectorLargeLimitStartsEmpty(t *testing.T) {
	collector := newUniqueIDCollector(1_000_000_000)

	if len(collector.ids) != 0 {
		t.Fatalf("expected no collected IDs, got %d", len(collector.ids))
	}
	if len(collector.seen) != 0 {
		t.Fatalf("expected no seen IDs, got %d", len(collector.seen))
	}
}

func TestUniqueIDCollectorStopsAtUniqueLimit(t *testing.T) {
	collector := newUniqueIDCollector(2)

	shouldContinue := collector.add([]string{"sm1", "sm1", "sm2", "sm3"})

	if shouldContinue {
		t.Fatal("expected collector to stop at the unique ID limit")
	}
	if !slices.Equal(collector.ids, []string{"sm1", "sm2"}) {
		t.Fatalf("expected two unique IDs, got %v", collector.ids)
	}
}
