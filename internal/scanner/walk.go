package scanner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/MarkusLitz/obdu/internal/model"
)

const MaxNoteParseBytes = 4 * 1024 * 1024 // 4 MiB

// NormalizeRelPath converts backslashes to forward slashes and resolves "." / ".." segments
// cleanly within a relative vault path. Returns "" if at root or if ".." would escape above root.
func NormalizeRelPath(p string) string {
	replaced := strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(replaced, "/")
	out := make([]string, 0, len(parts))
	for _, seg := range parts {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			if len(out) == 0 {
				return ""
			}
			out = out[:len(out)-1]
			continue
		}
		out = append(out, seg)
	}
	return strings.Join(out, "/")
}

// IsHiddenOrSystemSegment reports whether a single path segment marks a hidden or system entry.
func IsHiddenOrSystemSegment(seg string) bool {
	return strings.HasPrefix(seg, ".") || seg == "node_modules"
}

// HasHiddenOrSystemPath reports whether any segment in a slash-separated relative path is hidden/system.
func HasHiddenOrSystemPath(relPath string) bool {
	if relPath == "" {
		return false
	}
	for _, seg := range strings.Split(relPath, "/") {
		if IsHiddenOrSystemSegment(seg) {
			return true
		}
	}
	return false
}

// IsPrimaryNoteExt reports whether the lowercase extension (without dot) is a primary Obsidian note format.
func IsPrimaryNoteExt(ext string) bool {
	switch strings.ToLower(ext) {
	case "md", "canvas", "base":
		return true
	default:
		return false
	}
}

// FileExtLower extracts the lowercase file extension without leading dot.
// Dotfiles like ".DS_Store" return "".
func FileExtLower(name string) string {
	idx := strings.LastIndexByte(name, '.')
	if idx <= 0 || idx == len(name)-1 {
		return ""
	}
	return strings.ToLower(name[idx+1:])
}

// FindVaultRoot resolves startPath and walks upward looking for a ".obsidian" directory.
// If found, it returns (vaultRoot, initialRelPath, true, nil).
// If not found, it falls back to standard ncdu mode at (startDir, "", false, nil).
func FindVaultRoot(startPath string) (string, string, bool, error) {
	if strings.TrimSpace(startPath) == "" {
		startPath = "."
	}
	abs, err := filepath.Abs(startPath)
	if err != nil {
		return "", "", false, err
	}
	abs = filepath.Clean(abs)
	info, err := os.Lstat(abs)
	if err != nil {
		return "", "", false, err
	}
	startDir := abs
	if !info.IsDir() {
		startDir = filepath.Dir(abs)
	}

	cur := startDir
	for {
		obsCandidate := filepath.Join(cur, ".obsidian")
		if st, err := os.Lstat(obsCandidate); err == nil && st.IsDir() {
			rel, relErr := filepath.Rel(cur, startDir)
			initRel := ""
			if relErr == nil && rel != "." {
				initRel = NormalizeRelPath(filepath.ToSlash(rel))
			}
			return cur, initRel, true, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return startDir, "", false, nil
}

type bookmarksFile struct {
	Items []bookmarkItem `json:"items"`
}

type bookmarkItem struct {
	Type  string         `json:"type"`
	Path  string         `json:"path"`
	Items []bookmarkItem `json:"items"`
}

// LoadBookmarkedPaths reads .obsidian/bookmarks.json if present and returns normalized vault-relative paths.
func LoadBookmarkedPaths(vaultRoot string) map[string]bool {
	result := make(map[string]bool)
	bmPath := filepath.Join(vaultRoot, ".obsidian", "bookmarks.json")
	data, err := os.ReadFile(bmPath)
	if err != nil {
		return result
	}
	var parsed bookmarksFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return result
	}
	var collect func(items []bookmarkItem)
	collect = func(items []bookmarkItem) {
		for _, item := range items {
			if p := strings.TrimSpace(item.Path); p != "" {
				if norm := NormalizeRelPath(p); norm != "" {
					result[norm] = true
				}
			}
			if len(item.Items) > 0 {
				collect(item.Items)
			}
		}
	}
	collect(parsed.Items)
	return result
}

type readJob struct {
	index   int
	absPath string
}

// ScanRoot walks rootAbs using os.Lstat without following symlinks and reads active note contents
// using a bounded worker pool.
func ScanRoot(rootAbs string) ([]model.ScannedEntry, error) {
	cleanRoot, err := filepath.Abs(rootAbs)
	if err != nil {
		return nil, err
	}
	cleanRoot = filepath.Clean(cleanRoot)

	var entries []model.ScannedEntry
	var jobs []readJob

	type queueDir struct {
		absPath string
		relPath string
		hidden  bool
	}

	queue := []queueDir{{absPath: cleanRoot, relPath: "", hidden: false}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		dirEntries, err := os.ReadDir(cur.absPath)
		if err != nil {
			continue
		}

		for _, de := range dirEntries {
			// Never follow or traverse symlinks.
			if de.Type()&os.ModeSymlink != 0 {
				continue
			}
			name := de.Name()
			childAbs := filepath.Join(cur.absPath, name)
			childRel := name
			if cur.relPath != "" {
				childRel = cur.relPath + "/" + name
			}
			childHidden := cur.hidden || IsHiddenOrSystemSegment(name)

			fi, err := os.Lstat(childAbs)
			if err != nil {
				continue
			}
			if fi.Mode()&os.ModeSymlink != 0 {
				continue
			}

			if fi.IsDir() {
				entries = append(entries, model.ScannedEntry{
					RelPath:       childRel,
					AbsPath:       childAbs,
					Name:          name,
					IsDir:         true,
					ApparentBytes: 0,
					DiskBytes:     0,
					MTimeUnix:     fi.ModTime().Unix(),
				})
				queue = append(queue, queueDir{
					absPath: childAbs,
					relPath: childRel,
					hidden:  childHidden,
				})
				continue
			}

			if !fi.Mode().IsRegular() {
				continue
			}

			apparent := fi.Size()
			disk := computeDiskBytes(fi)
			idx := len(entries)
			entries = append(entries, model.ScannedEntry{
				RelPath:       childRel,
				AbsPath:       childAbs,
				Name:          name,
				IsDir:         false,
				ApparentBytes: apparent,
				DiskBytes:     disk,
				MTimeUnix:     fi.ModTime().Unix(),
			})

			ext := FileExtLower(name)
			if !childHidden && IsPrimaryNoteExt(ext) && apparent <= MaxNoteParseBytes {
				jobs = append(jobs, readJob{
					index:   idx,
					absPath: childAbs,
				})
			}
		}
	}

	if len(jobs) > 0 {
		workers := runtime.NumCPU()
		if workers > 16 {
			workers = 16
		}
		if workers < 2 {
			workers = 2
		}
		if workers > len(jobs) {
			workers = len(jobs)
		}

		jobCh := make(chan readJob, len(jobs))
		for _, j := range jobs {
			jobCh <- j
		}
		close(jobCh)

		var wg sync.WaitGroup
		wg.Add(workers)
		for w := 0; w < workers; w++ {
			go func() {
				defer wg.Done()
				for job := range jobCh {
					data, err := os.ReadFile(job.absPath)
					if err == nil {
						entries[job.index].Content = string(data)
						entries[job.index].HasContent = true
					}
				}
			}()
		}
		wg.Wait()
	}

	return entries, nil
}
