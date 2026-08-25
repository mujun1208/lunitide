package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

// Opening a store is the floor under almost every test in this tree, and under
// the race detector that floor is most of the CI budget. These separate the
// three costs inside it so the expensive one can be told from the cheap ones:
// replaying the whole manifest into a new file, reopening a file already at the
// latest version, and the post-migration audit on its own.

func BenchmarkOpenFreshDatabase(b *testing.B) {
	ctx := context.Background()
	dir := b.TempDir()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := Open(ctx, filepath.Join(dir, fmt.Sprintf("fresh-%d.db", i)))
		if err != nil {
			b.Fatal(err)
		}
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOpenMigratedDatabase(b *testing.B) {
	ctx := context.Background()
	path := filepath.Join(b.TempDir(), "migrated.db")
	seed, err := Open(ctx, path)
	if err != nil {
		b.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := Open(ctx, path)
		if err != nil {
			b.Fatal(err)
		}
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOpenTemplatedDatabase(b *testing.B) {
	ctx := context.Background()
	dir := b.TempDir()
	// Built outside the timer: the snapshot is a once-per-process cost, and
	// charging it to the first iteration would describe neither the first
	// caller nor the hundreds that follow.
	if _, err := migratedImage(ctx); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := OpenTemplated(ctx, filepath.Join(dir, fmt.Sprintf("templated-%d.db", i)))
		if err != nil {
			b.Fatal(err)
		}
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateSchema(b *testing.B) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(b.TempDir(), "validate.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := validateSchema(ctx, s.db); err != nil {
			b.Fatal(err)
		}
	}
}
