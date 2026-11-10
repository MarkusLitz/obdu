//go:build !unix

package scanner

import "os"

func computeDiskBytes(fi os.FileInfo) int64 {
	if fi == nil || fi.IsDir() {
		return 0
	}
	size := fi.Size()
	if size <= 0 {
		return 0
	}
	return ((size + 4095) / 4096) * 4096
}
