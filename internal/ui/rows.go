package ui

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/MarkusLitz/obdu/internal/graph"
	"github.com/MarkusLitz/obdu/internal/model"
	"github.com/MarkusLitz/obdu/internal/parser"
)

// compareNatural compares two strings case-insensitively with natural numeric chunk ordering.
func compareNatural(a, b string) int {
	ra := []rune(strings.ToLower(a))
	rb := []rune(strings.ToLower(b))
	ia, ib := 0, 0
	for ia < len(ra) && ib < len(rb) {
		ca, cb := ra[ia], rb[ib]
		if unicode.IsDigit(ca) && unicode.IsDigit(cb) {
			// Skip leading zeros
			za, zb := ia, ib
			for ia < len(ra) && ra[ia] == '0' {
				ia++
			}
			for ib < len(rb) && rb[ib] == '0' {
				ib++
			}
			startA, startB := ia, ib
			for ia < len(ra) && unicode.IsDigit(ra[ia]) {
				ia++
			}
			for ib < len(rb) && unicode.IsDigit(rb[ib]) {
				ib++
			}
			lenA := ia - startA
			lenB := ib - startB
			if lenA != lenB {
				if lenA < lenB {
					return -1
				}
				return 1
			}
			for k := 0; k < lenA; k++ {
				if ra[startA+k] != rb[startB+k] {
					if ra[startA+k] < rb[startB+k] {
						return -1
					}
					return 1
				}
			}
			zerosA := startA - za
			zerosB := startB - zb
			if zerosA != zerosB {
				if zerosA < zerosB {
					return -1
				}
				return 1
			}
			continue
		}
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
		ia++
		ib++
	}
	if len(ra) < len(rb) {
		return -1
	}
	if len(ra) > len(rb) {
		return 1
	}
	return strings.Compare(a, b)
}

func (a *App) compareBySort(nA, nB *model.Node, sizeA, sizeB int64) int {
	cmp := 0
	switch a.sortKey {
	case model.SortBySize:
		if sizeB > sizeA {
			cmp = 1
		} else if sizeB < sizeA {
			cmp = -1
		}
	case model.SortByName:
		if nA.Kind == model.KindDir && nB.Kind != model.KindDir {
			cmp = -1
		} else if nA.Kind != model.KindDir && nB.Kind == model.KindDir {
			cmp = 1
		} else {
			cmp = compareNatural(nA.Name, nB.Name)
		}
	case model.SortByItems:
		cmp = nB.VisibleItemCount - nA.VisibleItemCount
	case model.SortByMTime:
		if nB.MTimeUnix > nA.MTimeUnix {
			cmp = 1
		} else if nB.MTimeUnix < nA.MTimeUnix {
			cmp = -1
		}
	}
	if cmp == 0 {
		cmp = compareNatural(nA.RelPath, nB.RelPath)
	}
	if a.sortAsc {
		return -cmp
	}
	return cmp
}

func (a *App) sortNodes(nodes []*model.Node) {
	sort.Slice(nodes, func(i, j int) bool {
		return a.compareBySort(nodes[i], nodes[j], nodes[i].DisplayBytes, nodes[j].DisplayBytes) < 0
	})
}

func (a *App) computeRows() []model.DisplayRow {
	if a.graph == nil {
		return nil
	}
	var rawRows []model.DisplayRow
	switch a.viewTab {
	case model.TabTree:
		rawRows = a.buildTreeModeRows()
	case model.TabOrphans:
		rawRows = a.buildOrphansTabRows()
	case model.TabLargest:
		rawRows = a.buildLargestFilesRows()
	case model.TabIssues:
		rawRows = a.buildIssuesTabRows()
	}

	q := strings.ToLower(strings.TrimSpace(a.filterQuery))
	if q == "" {
		return rawRows
	}
	filtered := make([]model.DisplayRow, 0, len(rawRows))
	for _, r := range rawRows {
		if r.Role == model.RoleParent ||
			strings.Contains(strings.ToLower(r.Label), q) ||
			strings.Contains(strings.ToLower(r.Badge), q) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func (a *App) buildTreeModeRows() []model.DisplayRow {
	vg := a.graph
	target := vg.NodesByPath[a.currentPath]
	if target == nil {
		target = vg.Root
	}
	var rows []model.DisplayRow

	if len(a.navStack) > 0 || target.RelPath != "" {
		rows = append(rows, model.DisplayRow{
			Key:     "parent:..",
			Role:    model.RoleParent,
			Flag:    ' ',
			Bytes:   0,
			ShowBar: false,
			Label:   "/..",
			Badge:   "",
		})
	}

	if a.currentMode == model.ModeDir {
		children := make([]*model.Node, 0, len(target.Children))
		for _, c := range target.Children {
			if a.showHidden || !c.IsHidden {
				children = append(children, c)
			}
		}
		a.sortNodes(children)
		for _, child := range children {
			prefix := ""
			if child.Kind == model.KindDir {
				prefix = "/"
			}
			rows = append(rows, model.DisplayRow{
				Key:       "node:" + child.RelPath,
				Role:      model.RoleTreeNode,
				Flag:      graph.GetNodeFlag(child, vg.IsVault),
				Bytes:     child.DisplayBytes,
				ItemCount: child.VisibleItemCount,
				ShowBar:   true,
				Label:     prefix + parser.SanitizeDisplayString(child.Name),
				Badge:     graph.GetNodeBadge(child, a.sizeMode, vg.IsVault),
				Node:      child,
			})
		}
		return rows
	}

	if a.currentMode == model.ModeNote {
		selfBytes := graph.GetRawNodeSize(target, a.blockMode)
		selfFlag := ' '
		if target.EmptyReason != "" {
			selfFlag = 'e'
		}
		selfBadge := target.EmptyReason
		if selfBadge == "" {
			selfBadge = "Enter = Textvorschau / [o] Obsidian"
		}
		rows = append(rows, model.DisplayRow{
			Key:       "self:" + target.RelPath,
			Role:      model.RoleNoteSelf,
			Flag:      selfFlag,
			Bytes:     selfBytes,
			ItemCount: 1,
			ShowBar:   true,
			Label:     "[Notiztext] " + parser.SanitizeDisplayString(target.Name),
			Badge:     selfBadge,
			Node:      target,
		})

		for _, att := range target.OutgoingAttachments {
			share := graph.GetAttachmentShareForNote(att, target, a.blockMode)
			full := graph.GetRawNodeSize(att, a.blockMode)
			owners := len(att.IncomingNoteOwners)
			badge := att.RelPath
			flag := ' '
			if owners > 1 {
				flag = 'S'
				badge = fmt.Sprintf("shared (%dx, gesamt %s) · %s", owners, graph.FormatBytesNcdu(full), att.RelPath)
			}
			bytesToShow := full
			if a.sizeMode == model.SizeLogical {
				bytesToShow = share
			}
			rows = append(rows, model.DisplayRow{
				Key:       "att:" + att.RelPath,
				Role:      model.RoleNoteAttachment,
				Flag:      flag,
				Bytes:     bytesToShow,
				ItemCount: 1,
				ShowBar:   true,
				Label:     "[Anhang] " + parser.SanitizeDisplayString(att.Name),
				Badge:     badge,
				Node:      att,
			})
		}

		for i := range target.ExternalRefs {
			ext := target.ExternalRefs[i]
			tag := "Web-Medien"
			if ext.Provider == "gdrive" {
				tag = "Google Drive"
			}
			rows = append(rows, model.DisplayRow{
				Key:         "ext:" + ext.URL,
				Role:        model.RoleNoteExternal,
				Flag:        '@',
				Bytes:       0,
				ShowBar:     false,
				Label:       "[Extern] " + ext.Label,
				Badge:       tag + " (0 B lokal)",
				ExternalRef: &ext,
			})
		}

		for _, outNote := range target.OutgoingNotes {
			rows = append(rows, model.DisplayRow{
				Key:     "out:" + outNote.RelPath,
				Role:    model.RoleNoteOutgoing,
				Flag:    '>',
				Bytes:   outNote.DisplayBytes,
				ShowBar: false,
				Label:   "→ Verlinkte Notiz: " + parser.SanitizeDisplayString(outNote.RelPath),
				Badge:   "Ausgehender Link",
				Node:    outNote,
			})
		}

		for _, inNote := range target.IncomingNotes {
			rows = append(rows, model.DisplayRow{
				Key:     "in:" + inNote.RelPath,
				Role:    model.RoleNoteBacklink,
				Flag:    '<',
				Bytes:   inNote.DisplayBytes,
				ShowBar: false,
				Label:   "← Backlink von: " + parser.SanitizeDisplayString(inNote.RelPath),
				Badge:   "Eingehender Link",
				Node:    inNote,
			})
		}

		for _, broken := range target.BrokenLinks {
			rows = append(rows, model.DisplayRow{
				Key:          "broken:" + broken,
				Role:         model.RoleNoteBroken,
				Flag:         '!',
				Bytes:        0,
				ShowBar:      false,
				Label:        "! Fehlendes Ziel: " + parser.SanitizeDisplayString(broken),
				Badge:        "Kaputter Link",
				BrokenTarget: broken,
			})
		}
		return rows
	}

	// ModeAttachment: Attachment Reference View
	attBytes := graph.GetRawNodeSize(target, a.blockMode)
	attBadge := fmt.Sprintf("Eingebunden in %d Notiz(en)", len(target.IncomingNoteOwners))
	if len(target.IncomingNoteOwners) == 0 {
		attBadge = "ORPHAN – von keiner Notiz eingebunden"
	}
	rows = append(rows, model.DisplayRow{
		Key:       "self:" + target.RelPath,
		Role:      model.RoleNoteSelf,
		Flag:      graph.GetNodeFlag(target, vg.IsVault),
		Bytes:     attBytes,
		ItemCount: 1,
		ShowBar:   true,
		Label:     "[Datei] " + parser.SanitizeDisplayString(target.RelPath),
		Badge:     attBadge,
		Node:      target,
	})

	for _, owner := range target.IncomingNoteOwners {
		rows = append(rows, model.DisplayRow{
			Key:     "owner:" + owner.RelPath,
			Role:    model.RoleAttachmentOwner,
			Flag:    '<',
			Bytes:   owner.DisplayBytes,
			ShowBar: false,
			Label:   "← Verwendet in: " + parser.SanitizeDisplayString(owner.RelPath),
			Badge:   "Enter = zur Notiz springen",
			Node:    owner,
		})
	}
	return rows
}

func (a *App) buildOrphansTabRows() []model.DisplayRow {
	vg := a.graph
	var candidates []*model.Node
	for _, f := range vg.AllFiles {
		if f.IsHidden || f.Kind == model.KindSystem || f.IsBookmarked {
			continue
		}
		if f.Kind == model.KindAttachment && len(f.IncomingNoteOwners) == 0 {
			if a.orphanScope == model.OrphanAttachments || a.orphanScope == model.OrphanAll {
				candidates = append(candidates, f)
			}
		} else if f.Kind == model.KindNote && len(f.IncomingNotes) == 0 {
			if a.orphanScope == model.OrphanNotes || a.orphanScope == model.OrphanAll {
				candidates = append(candidates, f)
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		sa := graph.GetRawNodeSize(candidates[i], a.blockMode)
		sb := graph.GetRawNodeSize(candidates[j], a.blockMode)
		return a.compareBySort(candidates[i], candidates[j], sa, sb) < 0
	})

	rows := make([]model.DisplayRow, 0, len(candidates))
	for _, f := range candidates {
		flag := ' '
		badge := "0 backlinks"
		if f.Kind == model.KindAttachment {
			flag = 'O'
			badge = "orphan attachment"
		}
		rows = append(rows, model.DisplayRow{
			Key:       "orphan:" + f.RelPath,
			Role:      model.RoleTreeNode,
			Flag:      flag,
			Bytes:     graph.GetRawNodeSize(f, a.blockMode),
			ItemCount: 1,
			ShowBar:   true,
			Label:     parser.SanitizeDisplayString(f.RelPath),
			Badge:     badge,
			Node:      f,
		})
	}
	return rows
}

func (a *App) buildLargestFilesRows() []model.DisplayRow {
	vg := a.graph
	candidates := make([]*model.Node, 0, len(vg.AllFiles))
	for _, f := range vg.AllFiles {
		if a.showHidden || !f.IsHidden {
			candidates = append(candidates, f)
		}
	}

	fileBytes := func(n *model.Node) int64 {
		if a.sizeMode == model.SizeLogical && n.Kind == model.KindNote {
			return n.DisplayBytes
		}
		return graph.GetRawNodeSize(n, a.blockMode)
	}

	sort.Slice(candidates, func(i, j int) bool {
		sa := fileBytes(candidates[i])
		sb := fileBytes(candidates[j])
		if sa != sb {
			return sb < sa
		}
		return compareNatural(candidates[i].RelPath, candidates[j].RelPath) < 0
	})

	if len(candidates) > 200 {
		candidates = candidates[:200]
	}

	rows := make([]model.DisplayRow, 0, len(candidates))
	for _, node := range candidates {
		rows = append(rows, model.DisplayRow{
			Key:       "top:" + node.RelPath,
			Role:      model.RoleTreeNode,
			Flag:      graph.GetNodeFlag(node, vg.IsVault),
			Bytes:     fileBytes(node),
			ItemCount: 1,
			ShowBar:   true,
			Label:     parser.SanitizeDisplayString(node.RelPath),
			Badge:     graph.GetNodeBadge(node, a.sizeMode, vg.IsVault),
			Node:      node,
		})
	}
	return rows
}

func (a *App) buildIssuesTabRows() []model.DisplayRow {
	vg := a.graph
	var rows []model.DisplayRow

	type brokenGroup struct {
		target  string
		sources []*model.Node
	}
	brokenMap := make(map[string][]*model.Node)
	for _, f := range vg.AllFiles {
		if f.Kind == model.KindNote && !f.IsHidden && len(f.BrokenLinks) > 0 {
			for _, target := range f.BrokenLinks {
				brokenMap[target] = append(brokenMap[target], f)
			}
		}
	}
	groups := make([]brokenGroup, 0, len(brokenMap))
	for t, srcs := range brokenMap {
		groups = append(groups, brokenGroup{target: t, sources: srcs})
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].sources) != len(groups[j].sources) {
			return len(groups[i].sources) > len(groups[j].sources)
		}
		return compareNatural(groups[i].target, groups[j].target) < 0
	})

	for _, g := range groups {
		names := make([]string, 0, len(g.sources))
		for _, s := range g.sources {
			names = append(names, s.Name)
		}
		rows = append(rows, model.DisplayRow{
			Key:           "issue-broken:" + g.target,
			Role:          model.RoleDiagnosticBroken,
			Flag:          '!',
			Bytes:         0,
			ShowBar:       false,
			Label:         fmt.Sprintf("! Kaputter Link: „%s“", parser.SanitizeDisplayString(g.target)),
			Badge:         fmt.Sprintf("in %d Notiz(en): %s", len(g.sources), strings.Join(names, ", ")),
			Node:          g.sources[0],
			BrokenTarget:  g.target,
			BrokenSources: g.sources,
		})
	}

	var collectEmpty func(dir *model.Node)
	collectEmpty = func(dir *model.Node) {
		for _, c := range dir.Children {
			if c.IsHidden {
				continue
			}
			if c.EmptyReason != "" {
				prefix := ""
				if c.Kind == model.KindDir {
					prefix = "/"
				}
				rows = append(rows, model.DisplayRow{
					Key:     "issue-empty:" + c.RelPath,
					Role:    model.RoleDiagnosticEmpty,
					Flag:    'e',
					Bytes:   c.ApparentBytes,
					ShowBar: false,
					Label:   prefix + parser.SanitizeDisplayString(c.RelPath),
					Badge:   c.EmptyReason,
					Node:    c,
				})
			}
			if c.Kind == model.KindDir {
				collectEmpty(c)
			}
		}
	}
	collectEmpty(vg.Root)
	return rows
}
