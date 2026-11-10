//go:build unix

package scanner

import (
	"os"
	"syscall"
)

func computeDiskBytes(fi os.FileInfo) int64 {
	if fi == nil || fi.IsDir() {
		return 0
	}
	size := fi.Size()
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st != nil {
		if st.Blocks > 0 {
			return int64(st.Blocks) * 512
		}
	}
	if size <= 0 {
		return 0
	}
	return ((size + 4095) / 4096) * 4096
}
