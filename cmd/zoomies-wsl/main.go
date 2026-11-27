// Command zoomies-wsl is the program Windows scheduled tasks run to start a
// WSL2 distro without any window. It is built for the Windows GUI subsystem
// (-H windowsgui), embedded in the zoomies binary and written to
// %LOCALAPPDATA%\Zoomies by `zoomies agent --wsl-ensure`. See internal/wsl/silent
// for why it exists. Usage: zoomies-wsl.exe ensure <distro>.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/eyupio/zoomies/internal/wsl/silent"
)

const maxLog = 256 << 10

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	log, closeLog := openLog()
	defer closeLog()
	if len(args) != 2 || args[0] != "ensure" {
		fmt.Fprintln(log, "usage: zoomies-wsl.exe ensure <distro>")
		return 2
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	r := silent.Exec{Path: filepath.Join(root, "System32", "wsl.exe")}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return silent.Ensure(ctx, r, args[1], log)
}

// openLog appends to %LOCALAPPDATA%\Zoomies\wsl-launcher.log, which is the only
// place a window-less program can say anything. A log that cannot be opened
// must not stop the distro starting, so it falls back to discarding.
func openLog() (io.Writer, func()) {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		return io.Discard, func() {}
	}
	p := filepath.Join(dir, "Zoomies", "wsl-launcher.log")
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if st, err := os.Stat(p); err == nil && st.Size() > maxLog {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	f, err := os.OpenFile(p, flags, 0o644)
	if err != nil {
		return io.Discard, func() {}
	}
	w := &stamped{f}
	return w, func() { _ = f.Close() }
}

type stamped struct{ f *os.File }

func (s *stamped) Write(b []byte) (int, error) {
	if _, err := fmt.Fprintf(s.f, "%s ", time.Now().Format(time.RFC3339)); err != nil {
		return 0, err
	}
	return s.f.Write(b)
}
