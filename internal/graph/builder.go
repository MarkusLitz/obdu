package graph

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MarkusLitz/obdu/internal/model"
	"github.com/MarkusLitz/obdu/internal/parser"
	"github.com/MarkusLitz/obdu/internal/scanner"
)

func stripMdExtension(nameOrPath string) string {
	if strings.HasSuffix(strings.ToLower(nameOrPath), ".md") {
		return nameOrPath[:len(nameOrPath)-3]
	}
	return nameOrPath
}

// resolveRelativePath resolves a "./" or "../" link against baseFolder within the vault.
// If ".." traverses above the vault root, it returns ("", false).
func resolveRelativePath(baseFolder, relTarget string) (string, bool) {
	combined := relTarget
	if baseFolder != "" {
		combined = baseFolder + "/" + relTarget
	}
	parts := strings.Split(strings.ReplaceAll(combined, "\\", "/"), "/")
	stack := make([]string, 0, len(parts))
	for _, seg := range parts {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			if len(stack) == 0 {
				return "", false
			}
			stack = stack[:len(stack)-1]
			continue
		}
		stack = append(stack, seg)
	}
	if len(stack) == 0 {
		return "", false
	}
	return strings.Join(stack, "/"), true
}

func commonPrefixScore(dirA, dirB string) int {
	var a, b []string
	if dirA != "" {
		a = strings.Split(dirA, "/")
	}
	if dirB != "" {
		b = strings.Split(dirB, "/")
	}
	shared := 0
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		if strings.EqualFold(a[i], b[i]) {
			shared++
		} else {
			break
		}
	}
	diff := len(a) - len(b)
	if diff < 0 {
		diff = -diff
	}
	return shared*100 - diff
}

func pickClosestCandidate(candidates []*model.Node, sourceDir string) *model.Node {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	best := candidates[0]
	bestScore := -1_000_000
	for _, c := range candidates {
		cDir := ""
		if c.Parent != nil {
			cDir = c.Parent.RelPath
		}
		score := commonPrefixScore(sourceDir, cDir)
		if score > bestScore || (score == bestScore && c.RelPath < best.RelPath) {
			best = c
			bestScore = score
		}
	}
	return best
}

// BuildVaultGraph constructs the full directory hierarchy, resolves Obsidian links,
// computes orphan/empty diagnostics, and calculates both physical and logical sizes.
func BuildVaultGraph(
	vaultName string,
	vaultRoot string,
	isVault bool,
	entries []model.ScannedEntry,
	bookmarkedPaths map[string]bool,
) *model.VaultGraph {
	if vaultName == "" {
		vaultName = "/"
	}
	root := &model.Node{
		Name:             vaultName,
		RelPath:          "",
		AbsPath:          vaultRoot,
		Kind:             model.KindDir,
		IsHidden:         false,
		Children:         make([]*model.Node, 0),
		VisibleItemCount: 0,
	}

	nodesByPath := make(map[string]*model.Node, len(entries)+1)
	nodesByPath[""] = root

	var ensureDirNode func(dirRelPath string) *model.Node
	ensureDirNode = func(dirRelPath string) *model.Node {
		norm := scanner.NormalizeRelPath(dirRelPath)
		if existing, ok := nodesByPath[norm]; ok {
			return existing
		}
		slashIdx := strings.LastIndexByte(norm, '/')
		parentPath := ""
		name := norm
		if slashIdx >= 0 {
			parentPath = norm[:slashIdx]
			name = norm[slashIdx+1:]
		}
		parentNode := ensureDirNode(parentPath)
		hidden := parentNode.IsHidden || scanner.IsHiddenOrSystemSegment(name)
		absPath := vaultRoot
		if norm != "" && vaultRoot != "" {
			absPath = filepath.Join(vaultRoot, filepath.FromSlash(norm))
		}
		dirNode := &model.Node{
			Name:         name,
			RelPath:      norm,
			AbsPath:      absPath,
			Kind:         model.KindDir,
			IsHidden:     hidden,
			Parent:       parentNode,
			Children:     make([]*model.Node, 0),
			IsBookmarked: bookmarkedPaths[norm],
		}
		parentNode.Children = append(parentNode.Children, dirNode)
		nodesByPath[norm] = dirNode
		return dirNode
	}

	allFiles := make([]*model.Node, 0, len(entries))
	contentByPath := make(map[string]string)

	for _, entry := range entries {
		norm := scanner.NormalizeRelPath(entry.RelPath)
		if norm == "" {
			continue
		}
		if entry.IsDir {
			dirNode := ensureDirNode(norm)
			if entry.AbsPath != "" {
				dirNode.AbsPath = entry.AbsPath
			}
			if entry.MTimeUnix > dirNode.MTimeUnix {
				dirNode.MTimeUnix = entry.MTimeUnix
			}
			continue
		}
		if _, exists := nodesByPath[norm]; exists {
			continue
		}

		slashIdx := strings.LastIndexByte(norm, '/')
		parentPath := ""
		name := norm
		if slashIdx >= 0 {
			parentPath = norm[:slashIdx]
			name = norm[slashIdx+1:]
		}
		parentNode := ensureDirNode(parentPath)
		hidden := parentNode.IsHidden || scanner.IsHiddenOrSystemSegment(name)
		ext := scanner.FileExtLower(name)

		var kind model.NodeKind
		if hidden {
			kind = model.KindSystem
		} else if isVault && scanner.IsPrimaryNoteExt(ext) {
			kind = model.KindNote
		} else {
			kind = model.KindAttachment
		}

		diskBytes := entry.DiskBytes
		if diskBytes < 0 {
			if entry.ApparentBytes <= 0 {
				diskBytes = 0
			} else {
				diskBytes = ((entry.ApparentBytes + 4095) / 4096) * 4096
			}
		}

		absPath := entry.AbsPath
		if absPath == "" && vaultRoot != "" {
			absPath = filepath.Join(vaultRoot, filepath.FromSlash(norm))
		}

		emptyReason := ""
		if !hidden && entry.ApparentBytes == 0 {
			emptyReason = "0 Bytes"
		}

		fileNode := &model.Node{
			Name:             name,
			RelPath:          norm,
			AbsPath:          absPath,
			Ext:              ext,
			Kind:             kind,
			IsHidden:         hidden,
			ApparentBytes:    entry.ApparentBytes,
			DiskBytes:        diskBytes,
			MTimeUnix:        entry.MTimeUnix,
			Parent:           parentNode,
			IsBookmarked:     bookmarkedPaths[norm],
			EmptyReason:      emptyReason,
			DisplayBytes:     entry.ApparentBytes,
			VisibleItemCount: 1,
		}
		parentNode.Children = append(parentNode.Children, fileNode)
		nodesByPath[norm] = fileNode
		allFiles = append(allFiles, fileNode)

		if entry.HasContent || entry.Content != "" {
			contentByPath[norm] = entry.Content
		}
	}

	if isVault {
		// Index active (non-hidden, non-system) files for 4-tier Obsidian target resolution.
		activeByLowerPath := make(map[string]*model.Node, len(allFiles))
		activeByLowerName := make(map[string][]*model.Node)
		activeNotesByLowerStem := make(map[string][]*model.Node)

		for _, fileNode := range allFiles {
			if fileNode.IsHidden || fileNode.Kind == model.KindSystem {
				continue
			}
			lowerPath := strings.ToLower(fileNode.RelPath)
			activeByLowerPath[lowerPath] = fileNode

			lowerName := strings.ToLower(fileNode.Name)
			activeByLowerName[lowerName] = append(activeByLowerName[lowerName], fileNode)

			if fileNode.Kind == model.KindNote {
				lowerStem := strings.ToLower(stripMdExtension(fileNode.Name))
				activeNotesByLowerStem[lowerStem] = append(activeNotesByLowerStem[lowerStem], fileNode)
			}
		}

		resolveTarget := func(sourceNote *model.Node, rawTarget string) *model.Node {
			sourceDir := ""
			if sourceNote.Parent != nil {
				sourceDir = sourceNote.Parent.RelPath
			}

			// Tier 1: Explicit relative path (./ or ../)
			if strings.HasPrefix(rawTarget, "./") || strings.HasPrefix(rawTarget, "../") {
				relResolved, ok := resolveRelativePath(sourceDir, rawTarget)
				if !ok {
					return nil
				}
				lowerRel := strings.ToLower(relResolved)
				if hit, ok := activeByLowerPath[lowerRel]; ok {
					return hit
				}
				if hit, ok := activeByLowerPath[lowerRel+".md"]; ok {
					return hit
				}
				return nil
			}

			cleaned := strings.TrimLeft(rawTarget, "/")
			if cleaned == "" {
				return nil
			}
			lowerCleaned := strings.ToLower(cleaned)

			// Tier 2: Source-folder subpath
			if sourceDir != "" {
				fromSource := strings.ToLower(sourceDir) + "/" + lowerCleaned
				if hit, ok := activeByLowerPath[fromSource]; ok {
					return hit
				}
				if hit, ok := activeByLowerPath[fromSource+".md"]; ok {
					return hit
				}
			}

			// Tier 3: Vault-root path
			if hit, ok := activeByLowerPath[lowerCleaned]; ok {
				return hit
			}
			if hit, ok := activeByLowerPath[lowerCleaned+".md"]; ok {
				return hit
			}

			// Tier 4: Basename / shortest-path fallback (when target contains no "/")
			if !strings.ContainsRune(cleaned, '/') {
				if exactMatches := activeByLowerName[lowerCleaned]; len(exactMatches) > 0 {
					return pickClosestCandidate(exactMatches, sourceDir)
				}
				stemKey := strings.ToLower(stripMdExtension(lowerCleaned))
				if stemMatches := activeNotesByLowerStem[stemKey]; len(stemMatches) > 0 {
					return pickClosestCandidate(stemMatches, sourceDir)
				}
			}
			return nil
		}

		// Parse active notes and populate bidirectional link edges.
		for _, note := range allFiles {
			if note.Kind != model.KindNote || note.IsHidden {
				continue
			}
			content := contentByPath[note.RelPath]
			parsed := parser.ParseNoteContent(content, note.Ext)
			note.ExternalRefs = parsed.ExternalRefs
			note.EmptyReason = parsed.EmptyReason

			seenTargets := make(map[string]bool)
			seenBroken := make(map[string]bool)

			for _, rawTarget := range parsed.LocalTargets {
				targetNode := resolveTarget(note, rawTarget)
				if targetNode == nil {
					if !seenBroken[rawTarget] {
						seenBroken[rawTarget] = true
						note.BrokenLinks = append(note.BrokenLinks, rawTarget)
					}
					continue
				}
				if targetNode.RelPath == note.RelPath {
					continue
				}
				if seenTargets[targetNode.RelPath] {
					continue
				}
				seenTargets[targetNode.RelPath] = true

				switch targetNode.Kind {
				case model.KindNote:
					note.OutgoingNotes = append(note.OutgoingNotes, targetNode)
					targetNode.IncomingNotes = append(targetNode.IncomingNotes, note)
				case model.KindAttachment:
					note.OutgoingAttachments = append(note.OutgoingAttachments, targetNode)
					targetNode.IncomingNoteOwners = append(targetNode.IncomingNoteOwners, note)
				}
			}
		}

		// Sort reference slices deterministically (critical for exact remainder share attribution).
		for _, node := range allFiles {
			if len(node.IncomingNoteOwners) > 1 {
				sort.Slice(node.IncomingNoteOwners, func(i, j int) bool {
					return node.IncomingNoteOwners[i].RelPath < node.IncomingNoteOwners[j].RelPath
				})
			}
			if len(node.OutgoingAttachments) > 1 {
				sort.Slice(node.OutgoingAttachments, func(i, j int) bool {
					if node.OutgoingAttachments[i].ApparentBytes != node.OutgoingAttachments[j].ApparentBytes {
						return node.OutgoingAttachments[i].ApparentBytes > node.OutgoingAttachments[j].ApparentBytes
					}
					return node.OutgoingAttachments[i].RelPath < node.OutgoingAttachments[j].RelPath
				})
			}
			if len(node.OutgoingNotes) > 1 {
				sort.Slice(node.OutgoingNotes, func(i, j int) bool {
					return node.OutgoingNotes[i].RelPath < node.OutgoingNotes[j].RelPath
				})
			}
			if len(node.IncomingNotes) > 1 {
				sort.Slice(node.IncomingNotes, func(i, j int) bool {
					return node.IncomingNotes[i].RelPath < node.IncomingNotes[j].RelPath
				})
			}
		}
	}

	// Mark empty active directories.
	var markEmptyDirs func(dir *model.Node) bool
	markEmptyDirs = func(dir *model.Node) bool {
		if dir.IsHidden {
			return false
		}
		hasActiveChild := false
		for _, child := range dir.Children {
			if child.IsHidden {
				continue
			}
			hasActiveChild = true
			if child.Kind == model.KindDir {
				markEmptyDirs(child)
			}
		}
		if !hasActiveChild && dir.RelPath != "" {
			dir.EmptyReason = "Leerer Ordner"
		}
		return hasActiveChild
	}
	markEmptyDirs(root)

	orphanCount := 0
	var orphanBytes int64
	if isVault {
		for _, f := range allFiles {
			if f.Kind == model.KindAttachment && !f.IsHidden && len(f.IncomingNoteOwners) == 0 && !f.IsBookmarked {
				orphanCount++
				orphanBytes += f.ApparentBytes
			}
		}
	}

	RecomputeDisplaySizes(root, allFiles, model.SizeLogical, model.BlockApparent, true)
	totalLogical := root.DisplayBytes
	RecomputeDisplaySizes(root, allFiles, model.SizePhysical, model.BlockApparent, true)
	totalPhysical := root.DisplayBytes

	return &model.VaultGraph{
		VaultName:             vaultName,
		VaultRoot:             vaultRoot,
		IsVault:               isVault,
		Root:                  root,
		NodesByPath:           nodesByPath,
		AllFiles:              allFiles,
		TotalPhysicalBytes:    totalPhysical,
		TotalLogicalBytes:     totalLogical,
		OrphanAttachmentCount: orphanCount,
		OrphanAttachmentBytes: orphanBytes,
	}
}

// GetRawNodeSize returns either ApparentBytes or DiskBytes according to blockMode.
func GetRawNodeSize(node *model.Node, blockMode model.BlockMode) int64 {
	if blockMode == model.BlockDisk {
		return node.DiskBytes
	}
	return node.ApparentBytes
}

// GetAttachmentShareForNote computes the exact integer share of attachment's raw size attributed to ownerNote:
// floor(R(a) / K) + (1 if ownerIndex < (R(a) % K) else 0).
func GetAttachmentShareForNote(attachment *model.Node, ownerNote *model.Node, blockMode model.BlockMode) int64 {
	owners := attachment.IncomingNoteOwners
	k := int64(len(owners))
	if k == 0 {
		return 0
	}
	raw := GetRawNodeSize(attachment, blockMode)
	ownerIdx := -1
	for i, n := range owners {
		if n.RelPath == ownerNote.RelPath {
			ownerIdx = i
			break
		}
	}
	if ownerIdx < 0 {
		return 0
	}
	base := raw / k
	rem := raw % k
	if int64(ownerIdx) < rem {
		return base + 1
	}
	return base
}

// RecomputeDisplaySizes updates DisplayBytes and VisibleItemCount across the entire tree in memory (0 disk I/O).
func RecomputeDisplaySizes(
	root *model.Node,
	allFiles []*model.Node,
	sizeMode model.SizeMode,
	blockMode model.BlockMode,
	showHidden bool,
) {
	for _, file := range allFiles {
		if !showHidden && file.IsHidden {
			file.DisplayBytes = 0
			file.VisibleItemCount = 0
			continue
		}
		raw := GetRawNodeSize(file, blockMode)
		if sizeMode == model.SizePhysical {
			file.DisplayBytes = raw
		} else {
			if file.Kind == model.KindNote && !file.IsHidden {
				attributed := raw
				for _, att := range file.OutgoingAttachments {
					attributed += GetAttachmentShareForNote(att, file, blockMode)
				}
				file.DisplayBytes = attributed
			} else if file.Kind == model.KindAttachment && !file.IsHidden {
				if len(file.IncomingNoteOwners) > 0 {
					file.DisplayBytes = 0
				} else {
					file.DisplayBytes = raw
				}
			} else {
				file.DisplayBytes = raw
			}
		}
		file.VisibleItemCount = 1
	}

	var aggregateDir func(dir *model.Node)
	aggregateDir = func(dir *model.Node) {
		if !showHidden && dir.IsHidden {
			dir.DisplayBytes = 0
			dir.VisibleItemCount = 0
			return
		}
		var sumBytes int64
		count := 0
		maxMTime := dir.MTimeUnix
		for _, child := range dir.Children {
			if !showHidden && child.IsHidden {
				continue
			}
			if child.Kind == model.KindDir {
				aggregateDir(child)
				sumBytes += child.DisplayBytes
				count += child.VisibleItemCount
			} else {
				sumBytes += child.DisplayBytes
				count++
			}
			if child.MTimeUnix > maxMTime {
				maxMTime = child.MTimeUnix
			}
		}
		dir.DisplayBytes = sumBytes
		dir.VisibleItemCount = count
		dir.MTimeUnix = maxMTime
	}
	aggregateDir(root)
}

// FormatBytesNcdu formats a byte count in classic ncdu style (e.g. "110 B", "4.0 KiB", "48.2 MiB").
func FormatBytesNcdu(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	val := float64(bytes) / 1024.0
	u := 0
	for val >= 1024.0 && u < len(units)-1 {
		val /= 1024.0
		u++
	}
	return fmt.Sprintf("%.1f %s", val, units[u])
}

// GetNodeFlag returns the single-character ncdu status flag ('O', '!', 'S', 'e', or ' ').
// Critical domain rule: 'O' is only shown on unreferenced attachments in an Obsidian vault, never on notes.
func GetNodeFlag(node *model.Node, isVault bool) rune {
	if node == nil || node.IsHidden {
		return ' '
	}
	if node.Kind == model.KindDir {
		if node.EmptyReason != "" {
			return 'e'
		}
		return ' '
	}
	if !isVault {
		if node.EmptyReason != "" {
			return 'e'
		}
		return ' '
	}
	if node.Kind == model.KindNote {
		if len(node.BrokenLinks) > 0 {
			return '!'
		}
		if node.EmptyReason != "" {
			return 'e'
		}
		return ' '
	}
	if node.Kind == model.KindAttachment {
		if len(node.IncomingNoteOwners) == 0 && !node.IsBookmarked {
			return 'O'
		}
		if len(node.IncomingNoteOwners) >= 2 {
			return 'S'
		}
	}
	return ' '
}

// GetNodeBadge returns the right-aligned metadata badge for a tree row.
func GetNodeBadge(node *model.Node, sizeMode model.SizeMode, isVault bool) string {
	if node == nil || node.IsHidden {
		return ""
	}
	if !isVault {
		if node.EmptyReason != "" {
			return node.EmptyReason
		}
		return ""
	}
	if node.Kind == model.KindNote {
		var parts []string
		if len(node.OutgoingAttachments) > 0 {
			parts = append(parts, fmt.Sprintf("%d att", len(node.OutgoingAttachments)))
		}
		if len(node.ExternalRefs) > 0 {
			parts = append(parts, fmt.Sprintf("%d ext", len(node.ExternalRefs)))
		}
		if len(node.OutgoingNotes) > 0 || len(node.IncomingNotes) > 0 {
			parts = append(parts, fmt.Sprintf("%d→ %d←", len(node.OutgoingNotes), len(node.IncomingNotes)))
		}
		if len(node.BrokenLinks) > 0 {
			parts = append(parts, fmt.Sprintf("%d broken!", len(node.BrokenLinks)))
		}
		if len(parts) == 0 && node.EmptyReason != "" {
			return node.EmptyReason
		}
		return strings.Join(parts, " · ")
	}
	if node.Kind == model.KindAttachment {
		owners := len(node.IncomingNoteOwners)
		if owners == 0 {
			if node.IsBookmarked {
				return "bookmarked"
			}
			return "orphan"
		}
		if sizeMode == model.SizeLogical {
			if owners == 1 {
				return "zugeordnet → " + node.IncomingNoteOwners[0].Name
			}
			return fmt.Sprintf("shared (%dx)", owners)
		}
		if owners == 1 {
			return "→ " + node.IncomingNoteOwners[0].Name
		}
		return fmt.Sprintf("shared (%dx)", owners)
	}
	if node.EmptyReason == "Leerer Ordner" {
		return "leer"
	}
	return ""
}
