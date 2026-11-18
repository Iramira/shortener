package storage

import (
	"fmt"
	"sync"
	"testing"
)

func TestCollisionDoesNotChangeIndexes(t *testing.T) {
	m := New()
	first := Link{ID: "same-id", OriginalURL: "https://example.com"}
	if !m.Add("alice", first) {
		t.Fatal("first insert should succeed")
	}
	if m.Add("bob", Link{ID: first.ID, OriginalURL: "https://example.org"}) {
		t.Fatal("duplicate ID should be rejected")
	}
	if got, ok := m.Lookup(first.ID); !ok || got != first.OriginalURL {
		t.Fatalf("Lookup = (%q, %v), want (%q, true)", got, ok, first.OriginalURL)
	}
	if links := m.List("bob"); len(links) != 0 {
		t.Fatalf("failed insert changed Bob's list: %v", links)
	}
	links := m.List("alice")
	if len(links) != 1 || links[0] != first {
		t.Fatalf("Alice's list = %v, want [%v]", links, first)
	}
	links[0].OriginalURL = "modified outside storage"
	if m.List("alice")[0] != first {
		t.Fatal("changing the returned slice must not change storage")
	}
}

func TestConcurrentReadsAndWrites(t *testing.T) {
	m := New()
	const count = 32
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			link := Link{ID: fmt.Sprint(i), OriginalURL: "https://example.com/" + fmt.Sprint(i)}
			if !m.Add("alice", link) {
				t.Errorf("failed to insert ID %q", link.ID)
			}
			m.List("alice")
			if got, ok := m.Lookup(link.ID); !ok || got != link.OriginalURL {
				t.Errorf("Lookup(%q) = (%q, %v)", link.ID, got, ok)
			}
		}(i)
	}
	wg.Wait()
	if links := m.List("alice"); len(links) != count {
		t.Fatalf("stored %d links, want %d", len(links), count)
	}
}
