package gitmerge

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkMergeReadsTheChangedFiles times the part of a merge that grows with
// the files the target branch brought in: listing the merged tree's difference
// from the pull request head and reading each file's bytes, which the controller
// needs to build the merge commit on GitHub.
//
// It is the cost the one-diff-tree-and-one-cat-file reading was written to cut,
// so the scenario is checked before anything is timed: the merge must be clean
// and must report exactly the files the target added.
func BenchmarkMergeReadsTheChangedFiles(b *testing.B) {
	for _, files := range []int{50, 500} {
		b.Run(fmt.Sprintf("files=%d", files), func(b *testing.B) {
			dir := repo(b)
			write(b, dir, "common.txt", []byte("common\n"))
			ancestor := commit(b, dir, "base")
			mustGit(b, dir, "checkout", "-qb", "side")
			write(b, dir, "side.txt", []byte("side\n"))
			side := commit(b, dir, "side")
			mustGit(b, dir, "checkout", "-q", "-")
			for i := 0; i < files; i++ {
				write(b, dir, fmt.Sprintf("added/dir%d/file%d.txt", i%10, i), []byte(fmt.Sprintf("file %d\n", i)))
			}
			base := commit(b, dir, "target")

			request := Request{Dir: dir, Ancestor: ancestor, Base: base, Head: side}
			got, err := Merge(context.Background(), request)
			if err != nil {
				b.Fatal(err)
			}
			if len(got.Conflicts) != 0 || len(got.Unsupported) != 0 || len(got.Changes) != files {
				b.Fatalf("the scenario is not the one this benchmark names: %d conflicts, %d unsupported, %d changes, want a clean merge with %d",
					len(got.Conflicts), len(got.Unsupported), len(got.Changes), files)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Merge(context.Background(), request); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
