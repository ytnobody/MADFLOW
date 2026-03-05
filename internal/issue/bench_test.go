package issue

import (
	"fmt"
	"testing"
)

func BenchmarkStoreCreate(b *testing.B) {
	dir := b.TempDir()
	s := NewStore(dir)
	for b.Loop() {
		_, _ = s.Create("benchmark title", "benchmark body")
	}
}

func BenchmarkStoreGet(b *testing.B) {
	dir := b.TempDir()
	s := NewStore(dir)
	iss, err := s.Create("benchmark title", "benchmark body")
	if err != nil {
		b.Fatalf("setup failed: %v", err)
	}
	for b.Loop() {
		_, _ = s.Get(iss.ID)
	}
}

func BenchmarkStoreList10(b *testing.B) {
	dir := b.TempDir()
	s := NewStore(dir)
	for i := range 10 {
		_, _ = s.Create(fmt.Sprintf("title %d", i), "body")
	}
	for b.Loop() {
		_, _ = s.List(StatusFilter{})
	}
}

func BenchmarkStoreUpdate(b *testing.B) {
	dir := b.TempDir()
	s := NewStore(dir)
	iss, err := s.Create("benchmark title", "benchmark body")
	if err != nil {
		b.Fatalf("setup failed: %v", err)
	}
	n := 0
	for b.Loop() {
		iss.Title = fmt.Sprintf("title %d", n)
		n++
		_ = s.Update(iss)
	}
}

func BenchmarkHasComment(b *testing.B) {
	iss := &Issue{}
	for i := range 100 {
		iss.Comments = append(iss.Comments, Comment{ID: int64(i)})
	}
	for b.Loop() {
		_ = iss.HasComment(99)
	}
}
