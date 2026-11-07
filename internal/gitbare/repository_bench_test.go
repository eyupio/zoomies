package gitbare

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// BenchmarkWriteARepository times writing the objects of one commit's tree, which
// the controller does three times for every conflict repair it prepares.
//
// The scenario is checked before anything is timed: the tree must hash to the id
// the writer reports for it on a second write, and the repository must hold an
// object for every file, so a benchmark that wrote nothing cannot look fast.
func BenchmarkWriteARepository(b *testing.B) {
	for _, count := range []int{500, 5000} {
		b.Run(fmt.Sprintf("files=%d", count), func(b *testing.B) {
			body := strings.Repeat("package main // a typical line of source text\n", 40)
			files := make([]File, count)
			for i := range files {
				files[i] = File{Path: fmt.Sprintf("src/pkg%d/file%d.go", i%50, i), Content: []byte(fmt.Sprintf("// %d\n%s", i, body))}
			}

			check := b.TempDir()
			first, err := Write(check, []Commit{{Files: files}})
			if err != nil {
				b.Fatal(err)
			}
			second, err := Write(b.TempDir(), []Commit{{Files: files}})
			if err != nil || len(first) != 1 || first[0] != second[0] {
				b.Fatalf("the same tree gave different commits: %v, %v, %v", first, second, err)
			}
			objects := 0
			entries, _ := os.ReadDir(check + "/objects")
			for _, entry := range entries {
				inner, _ := os.ReadDir(check + "/objects/" + entry.Name())
				objects += len(inner)
			}
			if objects < count {
				b.Fatalf("the repository holds %d objects for %d files", objects, count)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				dir, err := os.MkdirTemp("", "gitbare-bench-")
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if _, err := Write(dir, []Commit{{Files: files}}); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				_ = os.RemoveAll(dir)
				b.StartTimer()
			}
		})
	}
}
