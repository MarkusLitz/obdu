package actions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MarkusLitz/obdu/internal/model"
	"github.com/MarkusLitz/obdu/internal/scanner"
)

const TrashRoot = ".trash"

// resolveDeepestExisting EvalSymlinks-resolves the deepest existing ancestor of absPath
// and appends any non-existent trailing path segments cleanly.
func resolveDeepestExisting(absPath string) (string, error) {
	cleaned := filepath.Clean(absPath)
	cur := cleaned
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return filepath.Clean(resolved), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("cannot resolve path ancestor: %s", absPath)
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

// EnsureInsideRoot strictly validates that candidatePath (either vault-relative or absolute)
// resolves inside vaultRoot, contains no null bytes or traversal segments, and does not target
// vaultRoot itself or ".obsidian".
func EnsureInsideRoot(vaultRoot, candidatePath string) (string, error) {
	if strings.TrimSpace(vaultRoot) == "" || strings.TrimSpace(candidatePath) == "" {
		return "", errors.New("leerer Pfad ist nicht erlaubt")
	}
	if strings.ContainsRune(vaultRoot, '\x00') || strings.ContainsRune(candidatePath, '\x00') {
		return "", errors.New("Null-Bytes im Pfad sind nicht erlaubt")
	}

	trimmed := strings.TrimSpace(candidatePath)
	if trimmed == "." || trimmed == ".." || trimmed == "/" || trimmed == "\\" {
		return "", errors.New("ungültiger Zielpfad")
	}

	absRoot, err := filepath.Abs(vaultRoot)
	if err != nil {
		return "", err
	}
	cleanRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", err
	}
	cleanRoot = filepath.Clean(cleanRoot)

	var rawTargetAbs string
	if filepath.IsAbs(trimmed) {
		// Check raw segments for ".." before cleaning
		for _, seg := range strings.Split(filepath.ToSlash(trimmed), "/") {
			if seg == ".." {
				return "", errors.New("Pfad-Traversal (..) nicht erlaubt")
			}
		}
		rawTargetAbs = filepath.Clean(trimmed)
	} else {
		if strings.ContainsRune(trimmed, '\\') {
			return "", errors.New("Backslashes im relativen Pfad nicht erlaubt")
		}
		for _, seg := range strings.Split(trimmed, "/") {
			if seg == "" || seg == "." || seg == ".." {
				return "", errors.New("ungültiges Pfadsegment")
			}
		}
		rawTargetAbs = filepath.Join(cleanRoot, filepath.FromSlash(trimmed))
	}

	// Resolve symlinks on the parent directory of rawTargetAbs (without following a final symlink if the target itself is a symlink).
	parentAbs := filepath.Dir(rawTargetAbs)
	resolvedParent, err := resolveDeepestExisting(parentAbs)
	if err != nil {
		return "", err
	}
	resolvedTarget := filepath.Join(resolvedParent, filepath.Base(rawTargetAbs))
	resolvedTarget = filepath.Clean(resolvedTarget)

	prefixWithSep := cleanRoot + string(filepath.Separator)
	if !strings.HasPrefix(resolvedTarget, prefixWithSep) {
		return "", fmt.Errorf("Pfad liegt außerhalb des Vault-Roots: %s", candidatePath)
	}

	rel, err := filepath.Rel(cleanRoot, resolvedTarget)
	if err != nil {
		return "", err
	}
	relSlash := filepath.ToSlash(rel)
	if relSlash == "" || relSlash == "." || strings.HasPrefix(relSlash, "..") {
		return "", errors.New("Vault-Root darf nicht verändert werden")
	}
	if strings.EqualFold(relSlash, ".obsidian") {
		return "", errors.New("der Ordner .obsidian ist geschützt")
	}

	if fi, err := os.Lstat(resolvedTarget); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("Symlinks werden aus Sicherheitsgründen nicht unterstützt")
		}
	}

	return resolvedTarget, nil
}

func collectSubtreeNodes(node *model.Node) []*model.Node {
	var out []*model.Node
	var walk func(n *model.Node)
	walk = func(n *model.Node) {
		if n == nil {
			return
		}
		out = append(out, n)
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(node)
	return out
}

// BuildDeletePlan analyzes a node (file or directory) and computes exclusive attachments,
// protected shared attachments, incoming backlinks from outside the deleted subtree, and reclaimable bytes.
func BuildDeletePlan(target *model.Node) *model.DeletePlan {
	if target == nil {
		return &model.DeletePlan{}
	}
	isInsideTrash := target.RelPath == TrashRoot || strings.HasPrefix(target.RelPath, TrashRoot+"/")
	subtree := collectSubtreeNodes(target)
	deletedPaths := make(map[string]bool, len(subtree))
	for _, n := range subtree {
		deletedPaths[n.RelPath] = true
	}

	exclusiveMap := make(map[string]*model.Node)
	sharedMap := make(map[string]*model.Node)
	backlinkMap := make(map[string]*model.Node)
	var targetFileBytes int64

	for _, item := range subtree {
		if item.Kind != model.KindDir {
			targetFileBytes += item.ApparentBytes
		}
		if item.Kind == model.KindNote {
			for _, att := range item.OutgoingAttachments {
				if deletedPaths[att.RelPath] {
					continue
				}
				usedOutside := false
				for _, owner := range att.IncomingNoteOwners {
					if !deletedPaths[owner.RelPath] {
						usedOutside = true
						break
					}
				}
				if usedOutside {
					sharedMap[att.RelPath] = att
				} else {
					exclusiveMap[att.RelPath] = att
				}
			}
			for _, inc := range item.IncomingNotes {
				if !deletedPaths[inc.RelPath] {
					backlinkMap[inc.RelPath] = inc
				}
			}
		} else if item.Kind == model.KindAttachment {
			for _, owner := range item.IncomingNoteOwners {
				if !deletedPaths[owner.RelPath] {
					backlinkMap[owner.RelPath] = owner
				}
			}
		}
	}

	exclusiveSlice := make([]*model.Node, 0, len(exclusiveMap))
	var exclusiveBytes int64
	for _, att := range exclusiveMap {
		exclusiveSlice = append(exclusiveSlice, att)
		exclusiveBytes += att.ApparentBytes
	}
	sort.Slice(exclusiveSlice, func(i, j int) bool {
		if exclusiveSlice[i].ApparentBytes != exclusiveSlice[j].ApparentBytes {
			return exclusiveSlice[i].ApparentBytes > exclusiveSlice[j].ApparentBytes
		}
		return exclusiveSlice[i].RelPath < exclusiveSlice[j].RelPath
	})

	sharedSlice := make([]*model.Node, 0, len(sharedMap))
	for _, att := range sharedMap {
		sharedSlice = append(sharedSlice, att)
	}
	sort.Slice(sharedSlice, func(i, j int) bool {
		return sharedSlice[i].RelPath < sharedSlice[j].RelPath
	})

	backlinkSlice := make([]*model.Node, 0, len(backlinkMap))
	for _, bl := range backlinkMap {
		backlinkSlice = append(backlinkSlice, bl)
	}
	sort.Slice(backlinkSlice, func(i, j int) bool {
		return backlinkSlice[i].RelPath < backlinkSlice[j].RelPath
	})

	return &model.DeletePlan{
		Target:                          target,
		IsInsideTrash:                   isInsideTrash,
		ExclusiveAttachments:            exclusiveSlice,
		SharedAttachments:               sharedSlice,
		IncomingBacklinks:               backlinkSlice,
		ReclaimableBytesTargetOnly:      targetFileBytes,
		ReclaimableBytesWithAttachments: targetFileBytes + exclusiveBytes,
	}
}

func moveToVaultTrash(vaultRoot, relPath string, now time.Time) error {
	srcAbs, err := EnsureInsideRoot(vaultRoot, relPath)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(srcAbs); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	normRel := scanner.NormalizeRelPath(relPath)
	destRel := TrashRoot + "/" + normRel
	destAbs, err := EnsureInsideRoot(vaultRoot, destRel)
	if err != nil {
		return err
	}

	if _, err := os.Lstat(destAbs); err == nil {
		stamp := now.Format("2006-01-02 15-04-05")
		dotIdx := strings.LastIndexByte(destRel, '.')
		slashIdx := strings.LastIndexByte(destRel, '/')
		if dotIdx > slashIdx+1 {
			destRel = fmt.Sprintf("%s (%s)%s", destRel[:dotIdx], stamp, destRel[dotIdx:])
		} else {
			destRel = fmt.Sprintf("%s (%s)", destRel, stamp)
		}
		destAbs, err = EnsureInsideRoot(vaultRoot, destRel)
		if err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(destAbs), 0o755); err != nil {
		return err
	}
	return os.Rename(srcAbs, destAbs)
}

func removePermanently(vaultRoot, relPath string) error {
	targetAbs, err := EnsureInsideRoot(vaultRoot, relPath)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(targetAbs); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.RemoveAll(targetAbs)
}

func isProtectedAttachmentRoot(lowerRel string) bool {
	// Protect any folder whose own basename is "attachments" or "_resources"
	base := filepath.Base(lowerRel)
	return base == "attachments" || base == "_resources"
}

func cleanupEmptyAttachmentFolder(vaultRoot, folderRelPath string) {
	norm := scanner.NormalizeRelPath(folderRelPath)
	lower := strings.ToLower(norm)
	if norm == "" || isProtectedAttachmentRoot(lower) || !strings.Contains(lower, "attachments/") {
		return
	}
	folderAbs, err := EnsureInsideRoot(vaultRoot, norm)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(folderAbs)
	if err != nil {
		return
	}
	for _, de := range entries {
		if strings.EqualFold(de.Name(), ".ds_store") {
			continue
		}
		// Still contains active files or subdirectories
		return
	}
	// Remove lingering .DS_Store if present so os.Remove succeeds
	for _, de := range entries {
		if strings.EqualFold(de.Name(), ".ds_store") {
			if dsAbs, err := EnsureInsideRoot(vaultRoot, norm+"/"+de.Name()); err == nil {
				_ = os.Remove(dsAbs)
			}
		}
	}
	_ = os.Remove(folderAbs)
}

// ExecuteDelete executes a DeletePlan by either moving items to .trash/<RelPath> or permanently deleting them,
// and then cleans up any newly emptied per-note attachment subfolders.
func ExecuteDelete(vaultRoot string, isVault bool, plan *model.DeletePlan, opts model.DeleteOptions) error {
	if plan == nil || plan.Target == nil {
		return errors.New("kein Löschziel angegeben")
	}
	if _, err := EnsureInsideRoot(vaultRoot, plan.Target.RelPath); err != nil {
		return err
	}

	itemsToDelete := []*model.Node{plan.Target}
	candidateParents := make(map[string]bool)

	if opts.IncludeExclusiveAttachments && len(plan.ExclusiveAttachments) > 0 {
		for _, att := range plan.ExclusiveAttachments {
			if _, err := EnsureInsideRoot(vaultRoot, att.RelPath); err == nil {
				itemsToDelete = append(itemsToDelete, att)
				if att.Parent != nil && att.Parent.RelPath != "" {
					candidateParents[att.Parent.RelPath] = true
				}
			}
		}
	}
	if plan.Target.Parent != nil && plan.Target.Parent.RelPath != "" {
		candidateParents[plan.Target.Parent.RelPath] = true
	}

	permanent := !isVault || plan.IsInsideTrash || opts.Permanent
	now := time.Now()

	for _, node := range itemsToDelete {
		if permanent {
			if err := removePermanently(vaultRoot, node.RelPath); err != nil {
				return err
			}
		} else {
			if err := moveToVaultTrash(vaultRoot, node.RelPath, now); err != nil {
				return err
			}
		}
	}

	for folderRel := range candidateParents {
		cleanupEmptyAttachmentFolder(vaultRoot, folderRel)
	}
	return nil
}
