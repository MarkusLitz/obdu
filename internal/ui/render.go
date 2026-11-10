package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/MarkusLitz/obdu/internal/graph"
	"github.com/MarkusLitz/obdu/internal/model"
	"github.com/MarkusLitz/obdu/internal/parser"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

const barInnerWidth = 12

func drawString(s tcell.Screen, x, y, maxX int, text string, style tcell.Style) int {
	col := x
	for _, r := range text {
		w := runewidth.RuneWidth(r)
		if w <= 0 {
			continue
		}
		if col+w > maxX {
			break
		}
		s.SetContent(col, y, r, nil, style)
		col += w
	}
	return col
}

func fillRect(s tcell.Screen, x, y, w, h int, ch rune, style tcell.Style) {
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			s.SetContent(col, row, ch, nil, style)
		}
	}
}

func truncateRunes(text string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	if runewidth.StringWidth(text) <= maxCols {
		return text
	}
	if maxCols <= 1 {
		return "…"
	}
	var b strings.Builder
	cols := 0
	for _, r := range text {
		rw := runewidth.RuneWidth(r)
		if cols+rw > maxCols-1 {
			break
		}
		b.WriteRune(r)
		cols += rw
	}
	b.WriteRune('…')
	return b.String()
}

func formatNcduBar(bytes, maxBytes, sumBytes int64, style model.BarStyle) string {
	if style == model.BarNone {
		return ""
	}
	pct := 0.0
	if sumBytes > 0 {
		pct = (float64(bytes) / float64(sumBytes)) * 100.0
	}
	pctStr := fmt.Sprintf("%5.1f%%", pct)

	filled := 0
	if maxBytes > 0 {
		filled = int(math.Round((float64(bytes) / float64(maxBytes)) * float64(barInnerWidth)))
	}
	if filled < 0 {
		filled = 0
	}
	if filled > barInnerWidth {
		filled = barInnerWidth
	}
	barStr := "[" + strings.Repeat("#", filled) + strings.Repeat(" ", barInnerWidth-filled) + "]"

	switch style {
	case model.BarBoth:
		return pctStr + " " + barStr
	case model.BarHashOnly:
		return barStr
	case model.BarPercentOnly:
		return "[" + pctStr + "]"
	default:
		return ""
	}
}

func (a *App) describeLocation() string {
	if a.graph == nil {
		return "/"
	}
	vaultPrefix := "/" + a.graph.Root.Name
	switch a.viewTab {
	case model.TabOrphans:
		scopeName := "Attachments"
		if a.orphanScope == model.OrphanNotes {
			scopeName = "Notes (0 Backlinks)"
		} else if a.orphanScope == model.OrphanAll {
			scopeName = "All"
		}
		return fmt.Sprintf("%s :: Orphans [t:%s]", vaultPrefix, scopeName)
	case model.TabLargest:
		return vaultPrefix + " :: Top Largest Files"
	case model.TabIssues:
		return vaultPrefix + " :: Broken Links & Empty Items"
	}
	suffix := ""
	if a.currentPath != "" {
		suffix = "/" + a.currentPath
	}
	if a.currentMode == model.ModeNote {
		return vaultPrefix + suffix + " [Notiz-Inspektion]"
	}
	if a.currentMode == model.ModeAttachment {
		return vaultPrefix + suffix + " [Attachment-Referenzen]"
	}
	return vaultPrefix + suffix
}

func (a *App) render() {
	s := a.screen
	if s == nil {
		return
	}
	s.Clear()
	width, height := s.Size()
	if width < 20 || height < 5 {
		drawString(s, 0, 0, width, "Terminal too small", tcell.StyleDefault)
		s.Show()
		return
	}

	styleBase := tcell.StyleDefault
	styleTopBar := tcell.StyleDefault.Background(tcell.ColorTeal).Foreground(tcell.ColorBlack).Bold(true)
	styleSubBar := tcell.StyleDefault.Background(tcell.Color236).Foreground(tcell.ColorWhite)
	styleTabActive := tcell.StyleDefault.Background(tcell.ColorYellow).Foreground(tcell.ColorBlack).Bold(true)
	styleFooter := tcell.StyleDefault.Background(tcell.ColorTeal).Foreground(tcell.ColorBlack)
	styleSelected := tcell.StyleDefault.Background(tcell.ColorGreen).Foreground(tcell.ColorBlack).Bold(true)

	// 1. Top Header Bar (Row 0)
	fillRect(s, 0, 0, width, 1, ' ', styleTopBar)
	leftHeader := fmt.Sprintf(" obdu %s ~ Arrow keys / j k to navigate, ? for help", Version)
	drawString(s, 0, 0, width, leftHeader, styleTopBar)

	modeTag := "[m:PHYSICAL]"
	if a.sizeMode == model.SizeLogical {
		modeTag = "[m:LOGICAL+ATT]"
	}
	blockTag := "[a:BYTES]"
	if a.blockMode == model.BlockDisk {
		blockTag = "[a:BLOCKS]"
	}
	dotTag := "[.:DOTS OFF]"
	if a.showHidden {
		dotTag = "[.:DOTS ON]"
	}
	rightPills := fmt.Sprintf("%s %s %s ", modeTag, blockTag, dotTag)
	pillsWidth := runewidth.StringWidth(rightPills)
	if width > pillsWidth+10 {
		drawString(s, width-pillsWidth, 0, width, rightPills, styleTopBar)
	}

	// 2. Subbar: Tabs + Path (Row 1)
	fillRect(s, 0, 1, width, 1, '-', styleSubBar)
	col := 1
	orphanCnt := 0
	if a.graph != nil {
		orphanCnt = a.graph.OrphanAttachmentCount
	}
	tabs := []struct {
		tab   model.ViewTab
		label string
	}{
		{model.TabTree, fmt.Sprintf("[1%sTree]", ternaryStr(a.viewTab == model.TabTree, "*", ":"))},
		{model.TabOrphans, fmt.Sprintf("[2%sOrphans:%d]", ternaryStr(a.viewTab == model.TabOrphans, "*", ":"), orphanCnt)},
		{model.TabLargest, fmt.Sprintf("[3%sTop]", ternaryStr(a.viewTab == model.TabLargest, "*", ":"))},
		{model.TabIssues, fmt.Sprintf("[4%sIssues]", ternaryStr(a.viewTab == model.TabIssues, "*", ":"))},
	}
	for _, tb := range tabs {
		st := styleSubBar
		if a.viewTab == tb.tab {
			st = styleTabActive
		}
		col = drawString(s, col, 1, width, tb.label, st)
		col = drawString(s, col, 1, width, " ", styleSubBar)
	}
	locText := fmt.Sprintf("--- %s ", a.describeLocation())
	drawString(s, col, 1, width, locText, styleSubBar.Bold(true))

	// 3. List Viewport (Rows 2 .. height-2)
	listStartY := 2
	listHeight := height - 3
	if listHeight < 1 {
		listHeight = 1
	}

	// Adjust scroll offset so selectedIndex is visible
	if a.selectedIndex < a.topOffset {
		a.topOffset = a.selectedIndex
	}
	if a.selectedIndex >= a.topOffset+listHeight {
		a.topOffset = a.selectedIndex - listHeight + 1
	}
	if a.topOffset < 0 {
		a.topOffset = 0
	}

	var maxBytes, sumBytes int64
	for _, r := range a.rows {
		if r.ShowBar {
			if r.Bytes > maxBytes {
				maxBytes = r.Bytes
			}
			sumBytes += r.Bytes
		}
	}

	if len(a.rows) == 0 {
		drawString(s, 2, listStartY+1, width-2, "Keine Einträge in dieser Ansicht.", styleBase.Dim(true))
	}

	for i := 0; i < listHeight; i++ {
		rowIdx := a.topOffset + i
		if rowIdx >= len(a.rows) {
			break
		}
		row := a.rows[rowIdx]
		y := listStartY + i
		isSelected := rowIdx == a.selectedIndex

		rowStyle := styleBase
		if isSelected {
			rowStyle = styleSelected
			fillRect(s, 0, y, width, 1, ' ', rowStyle)
		}

		// Column 1: Flag (2 cols: " X")
		flagStyle := rowStyle
		if !isSelected {
			switch row.Flag {
			case 'O':
				flagStyle = styleBase.Foreground(tcell.ColorYellow).Bold(true)
			case '!':
				flagStyle = styleBase.Foreground(tcell.ColorRed).Bold(true)
			case 'S':
				flagStyle = styleBase.Foreground(tcell.ColorAqua).Bold(true)
			case 'e':
				flagStyle = styleBase.Dim(true)
			case '@', '>', '<':
				flagStyle = styleBase.Foreground(tcell.ColorAqua)
			}
		}
		s.SetContent(1, y, row.Flag, nil, flagStyle)
		cx := 3

		// Column 2: Size (10 cols right-aligned)
		sizeStr := ""
		if row.Role != model.RoleParent {
			sizeStr = graph.FormatBytesNcdu(row.Bytes)
		}
		paddedSize := fmt.Sprintf("%10s", sizeStr)
		cx = drawString(s, cx, y, width, paddedSize+"  ", rowStyle)

		// Column 3: Bar
		if a.barStyle != model.BarNone {
			barSample := formatNcduBar(0, 1, 1, a.barStyle)
			barColWidth := runewidth.StringWidth(barSample)
			if row.ShowBar {
				barText := formatNcduBar(row.Bytes, maxBytes, sumBytes, a.barStyle)
				cx = drawString(s, cx, y, width, barText+"  ", rowStyle)
			} else {
				cx = drawString(s, cx, y, width, strings.Repeat(" ", barColWidth+2), rowStyle)
			}
		}

		// Column 4: Optional Item Count ('c')
		if a.showCounts {
			cntStr := ""
			if row.Role != model.RoleParent && row.ItemCount > 0 {
				cntStr = fmt.Sprintf("%6d", row.ItemCount)
			} else {
				cntStr = "      "
			}
			cx = drawString(s, cx, y, width, cntStr+"  ", rowStyle)
		}

		// Column 5 & 6: Label (left) and Badge (right-aligned)
		badgeText := parser.SanitizeDisplayString(row.Badge)
		badgeWidth := runewidth.StringWidth(badgeText)
		availForLabel := width - cx - 1
		if badgeWidth > 0 && availForLabel > badgeWidth+6 {
			availForLabel = width - cx - badgeWidth - 3
		}

		labelStyle := rowStyle
		if !isSelected {
			if row.Role == model.RoleParent || (row.Node != nil && row.Node.Kind == model.KindDir) {
				labelStyle = styleBase.Foreground(tcell.ColorDodgerBlue).Bold(true)
			} else if row.Node != nil && row.Node.IsHidden {
				labelStyle = styleBase.Dim(true)
			} else if row.Node != nil && row.Node.Kind == model.KindNote {
				labelStyle = styleBase.Bold(true)
			}
		}
		truncLabel := truncateRunes(row.Label, availForLabel)
		drawString(s, cx, y, width-1, truncLabel, labelStyle)

		if badgeWidth > 0 && width-badgeWidth-1 > cx+runewidth.StringWidth(truncLabel)+1 {
			badgeStyle := rowStyle
			if !isSelected {
				if strings.Contains(badgeText, "broken!") || strings.Contains(badgeText, "ORPHAN") {
					badgeStyle = styleBase.Foreground(tcell.ColorRed).Bold(true)
				} else if badgeText == "orphan" {
					badgeStyle = styleBase.Foreground(tcell.ColorYellow)
				} else {
					badgeStyle = styleBase.Dim(true)
				}
			}
			drawString(s, width-badgeWidth-1, y, width, badgeText, badgeStyle)
		}
	}

	// 4. Bottom Footer / Filter Bar (Row height-1)
	footerY := height - 1
	fillRect(s, 0, footerY, width, 1, ' ', styleFooter)
	if a.filterActive || a.filterQuery != "" {
		cursorSuffix := ""
		if a.filterActive {
			cursorSuffix = "█"
		}
		filterLine := fmt.Sprintf(" /%s%s   (Enter/↑/↓ = Liste, Esc = Filter löschen)", a.filterQuery, cursorSuffix)
		drawString(s, 0, footerY, width, filterLine, styleFooter.Bold(true))
	} else if a.statusMsg != "" {
		drawString(s, 0, footerY, width, " "+a.statusMsg, styleFooter.Bold(true))
	} else if a.graph != nil {
		sortArrow := "v"
		if a.sortAsc {
			sortArrow = "^"
		}
		footerText := fmt.Sprintf(
			" Total: %s | Orphans: %d (%s) | Sort: %s %s | [i]Info [p]Preview [o]Obsidian [f]Finder [d]Del",
			graph.FormatBytesNcdu(a.graph.Root.DisplayBytes),
			a.graph.OrphanAttachmentCount,
			graph.FormatBytesNcdu(a.graph.OrphanAttachmentBytes),
			a.sortKey,
			sortArrow,
		)
		drawString(s, 0, footerY, width, footerText, styleFooter)
	}

	// 5. Active Modal Overlay (if any)
	if a.activeModal != modalNone {
		a.renderModalOverlay(width, height)
	}

	s.Show()
}

func (a *App) renderModalOverlay(screenW, screenH int) {
	boxW := screenW - 8
	if boxW > 92 {
		boxW = 92
	}
	if boxW < 36 {
		boxW = screenW - 2
	}
	boxH := screenH - 4
	if boxH > 26 {
		boxH = 26
	}
	if boxH < 8 {
		boxH = screenH - 2
	}
	startX := (screenW - boxW) / 2
	startY := (screenH - boxH) / 2

	borderColor := tcell.ColorTeal
	if a.activeModal == modalDelete {
		borderColor = tcell.ColorRed
	}
	styleBox := tcell.StyleDefault.Background(tcell.Color235).Foreground(tcell.ColorWhite)
	styleBorder := styleBox.Foreground(borderColor).Bold(true)
	styleTitle := tcell.StyleDefault.Background(borderColor).Foreground(tcell.ColorBlack).Bold(true)

	fillRect(a.screen, startX, startY, boxW, boxH, ' ', styleBox)

	// Draw box border
	for x := startX; x < startX+boxW; x++ {
		a.screen.SetContent(x, startY, '─', nil, styleBorder)
		a.screen.SetContent(x, startY+boxH-1, '─', nil, styleBorder)
	}
	for y := startY; y < startY+boxH; y++ {
		a.screen.SetContent(startX, y, '│', nil, styleBorder)
		a.screen.SetContent(startX+boxW-1, y, '│', nil, styleBorder)
	}
	a.screen.SetContent(startX, startY, '┌', nil, styleBorder)
	a.screen.SetContent(startX+boxW-1, startY, '┐', nil, styleBorder)
	a.screen.SetContent(startX, startY+boxH-1, '└', nil, styleBorder)
	a.screen.SetContent(startX+boxW-1, startY+boxH-1, '┘', nil, styleBorder)

	title := " " + a.modalTitle + " "
	drawString(a.screen, startX+2, startY, startX+boxW-2, truncateRunes(title, boxW-4), styleTitle)

	innerW := boxW - 4
	innerH := boxH - 3
	maxScroll := len(a.modalLines) - innerH
	if maxScroll < 0 {
		maxScroll = 0
	}
	if a.modalScroll > maxScroll {
		a.modalScroll = maxScroll
	}
	if a.modalScroll < 0 {
		a.modalScroll = 0
	}

	for i := 0; i < innerH; i++ {
		lineIdx := a.modalScroll + i
		if lineIdx >= len(a.modalLines) {
			break
		}
		line := a.modalLines[lineIdx]
		st := styleBox
		if strings.HasPrefix(line, "⚠") || strings.HasPrefix(line, "WARNUNG") {
			st = styleBox.Foreground(tcell.ColorRed).Bold(true)
		} else if strings.HasPrefix(line, "[x]") || strings.HasPrefix(line, "[ ]") {
			st = styleBox.Foreground(tcell.ColorYellow).Bold(true)
		}
		drawString(a.screen, startX+2, startY+1+i, startX+boxW-2, truncateRunes(line, innerW), st)
	}

	footerHint := " [Esc / q] Schließen  [↑/↓] Scrollen "
	if a.activeModal == modalDelete {
		footerHint = " [y / Enter] Ausführen  [a] Exkl. Attachments  [x] Permanent  [n / Esc] Abbrechen "
	}
	drawString(a.screen, startX+2, startY+boxH-1, startX+boxW-2, truncateRunes(footerHint, boxW-4), styleTitle)
}

func (a *App) buildInfoModalLines(node *model.Node) (string, []string) {
	title := "Datei- & Referenz-Info: " + parser.SanitizeDisplayString(node.Name)
	var lines []string
	rel := node.RelPath
	if rel == "" {
		rel = "/"
	}
	lines = append(lines, fmt.Sprintf("Vault-Pfad:            %s", parser.SanitizeDisplayString(rel)))
	lines = append(lines, fmt.Sprintf("Typ:                   %s", describeNodeKind(node)))
	lines = append(lines, fmt.Sprintf("Physische Größe:       %s (%d Bytes)", graph.FormatBytesNcdu(node.ApparentBytes), node.ApparentBytes))
	lines = append(lines, fmt.Sprintf("Block-Größe (Disk):    %s", graph.FormatBytesNcdu(node.DiskBytes)))

	if node.Kind == model.KindNote {
		logical := node.ApparentBytes
		for _, att := range node.OutgoingAttachments {
			logical += graph.GetAttachmentShareForNote(att, node, a.blockMode)
		}
		lines = append(lines, fmt.Sprintf("Logische Gesamtgröße:  %s", graph.FormatBytesNcdu(logical)))
	}
	if node.MTimeUnix > 0 {
		tStr := time.Unix(node.MTimeUnix, 0).Format("2006-01-02 15:04:05")
		lines = append(lines, fmt.Sprintf("Geändert am:           %s", tStr))
	}
	if node.IsBookmarked {
		lines = append(lines, "Lesezeichen:           Ja (vor Orphan-Warnung geschützt)")
	}
	if node.EmptyReason != "" {
		lines = append(lines, fmt.Sprintf("Leer-Status:           %s", node.EmptyReason))
	}
	lines = append(lines, "")

	if node.Kind == model.KindNote {
		lines = append(lines, fmt.Sprintf("Lokale Attachments:    %d", len(node.OutgoingAttachments)))
		for _, att := range node.OutgoingAttachments {
			lines = append(lines, fmt.Sprintf("  • %s (%s)", parser.SanitizeDisplayString(att.RelPath), graph.FormatBytesNcdu(att.ApparentBytes)))
		}
		lines = append(lines, fmt.Sprintf("Externe Medien/Drive:  %d", len(node.ExternalRefs)))
		for _, ext := range node.ExternalRefs {
			lines = append(lines, fmt.Sprintf("  • [%s] %s", ext.Provider, ext.Label))
		}
		lines = append(lines, fmt.Sprintf("Notiz-Referenzen:      %d ausgehend (→), %d eingehend (←)", len(node.OutgoingNotes), len(node.IncomingNotes)))
		if len(node.BrokenLinks) > 0 {
			lines = append(lines, fmt.Sprintf("Kaputte Links (!):     %s", parser.SanitizeDisplayString(strings.Join(node.BrokenLinks, ", "))))
		}
	} else if node.Kind == model.KindAttachment {
		if len(node.IncomingNoteOwners) == 0 {
			lines = append(lines, "Referenzen:            0 Notizen (ORPHAN – verwaiste Datei)")
		} else {
			lines = append(lines, fmt.Sprintf("Verwendet in (%d):", len(node.IncomingNoteOwners)))
			for _, ow := range node.IncomingNoteOwners {
				lines = append(lines, "  ← "+parser.SanitizeDisplayString(ow.RelPath))
			}
		}
	}
	return title, lines
}

func (a *App) buildDeleteModalLines() (string, []string) {
	plan := a.deletePlan
	if plan == nil || plan.Target == nil {
		return "Löschen", nil
	}
	title := "Smart Delete bestätigen: " + parser.SanitizeDisplayString(plan.Target.Name)
	var lines []string
	lines = append(lines, "Pfad: "+parser.SanitizeDisplayString(plan.Target.RelPath))
	lines = append(lines, "")

	if len(plan.IncomingBacklinks) > 0 {
		lines = append(lines, fmt.Sprintf("⚠ WARNUNG: Wird noch von %d aktiven Notiz(en) referenziert!", len(plan.IncomingBacklinks)))
		limit := len(plan.IncomingBacklinks)
		if limit > 6 {
			limit = 6
		}
		for i := 0; i < limit; i++ {
			lines = append(lines, "  • "+parser.SanitizeDisplayString(plan.IncomingBacklinks[i].RelPath))
		}
		if len(plan.IncomingBacklinks) > 6 {
			lines = append(lines, fmt.Sprintf("  … sowie %d weitere", len(plan.IncomingBacklinks)-6))
		}
		lines = append(lines, "")
	}

	if len(plan.ExclusiveAttachments) > 0 {
		box := "[ ]"
		if a.deleteOpts.IncludeExclusiveAttachments {
			box = "[x]"
		}
		exBytes := plan.ReclaimableBytesWithAttachments - plan.ReclaimableBytesTargetOnly
		lines = append(lines, fmt.Sprintf("%s [Taste a] %d exklusive Attachment(s) (%s) gleich mitlöschen:",
			box, len(plan.ExclusiveAttachments), graph.FormatBytesNcdu(exBytes)))
		limit := len(plan.ExclusiveAttachments)
		if limit > 5 {
			limit = 5
		}
		for i := 0; i < limit; i++ {
			att := plan.ExclusiveAttachments[i]
			lines = append(lines, fmt.Sprintf("    - %s (%s)", parser.SanitizeDisplayString(att.RelPath), graph.FormatBytesNcdu(att.ApparentBytes)))
		}
		if len(plan.ExclusiveAttachments) > 5 {
			lines = append(lines, fmt.Sprintf("    … +%d weitere", len(plan.ExclusiveAttachments)-5))
		}
		lines = append(lines, "")
	}

	if len(plan.SharedAttachments) > 0 {
		lines = append(lines, fmt.Sprintf("ℹ %d geteilte(s) Attachment(s) bleiben geschützt (noch in anderen Notizen verwendet).", len(plan.SharedAttachments)))
		lines = append(lines, "")
	}

	if !plan.IsInsideTrash && a.graph != nil && a.graph.IsVault {
		pBox := "[ ]"
		if a.deleteOpts.Permanent {
			pBox = "[x]"
		}
		lines = append(lines, fmt.Sprintf("%s [Taste x] Endgültig löschen statt sicher in „.trash/“ verschieben", pBox))
	} else {
		lines = append(lines, "⚠ Eintrag wird ENDGÜLTIG von der Festplatte gelöscht!")
	}

	reclaim := plan.ReclaimableBytesTargetOnly
	if a.deleteOpts.IncludeExclusiveAttachments {
		reclaim = plan.ReclaimableBytesWithAttachments
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Freigegebener Speicherumfang: %s", graph.FormatBytesNcdu(reclaim)))
	return title, lines
}

func buildHelpModalLines() (string, []string) {
	lines := []string{
		"↑ / ↓ oder k / j       Zeile hoch / runter (auch PgUp, PgDn, Home, End)",
		"Enter / → / l          Öffnen: Ordner, Notiz-Referenzen oder Attachment-Verwender",
		"Backspace / ← / h      Eine Ebene zurück in der Navigationshistorie",
		"m oder Tab             Größenmodus: Physisch ↔ Logisch (inkl. Attachments, bilanzneutral)",
		"a                      Apparent Size (Bytes) ↔ Disk-Blöcke (st_blocks * 512)",
		". oder e               System-/Dot-Ordner (.obsidian, .trash etc.) ein-/ausblenden",
		"c                      Elementanzahl pro Ordner ein-/ausblenden (wie ncdu)",
		"g                      Balkenstil wechseln ([% + #####] / [#####] / [%] / Aus)",
		"s / n / C / M          Sortieren nach Größe, Name, Anzahl, Änderungsdatum",
		"1 / 2 / 3 / 4          Ansicht: 1 Baum · 2 Orphans · 3 Größte Dateien · 4 Probleme",
		"t (in Ansicht 2)       Orphan-Filter umschalten: Attachments → Notizen → Alle",
		"/                      Live-Filter in aktueller Ansicht (Esc leert Filter)",
		"i                      Datei- & Referenz-Info-Modal öffnen",
		"p oder Space           Textvorschau (<= 512 KiB) oder QuickLook/System-Viewer",
		"o                      Ausgewählte Notiz/Datei direkt in Obsidian öffnen",
		"f                      Im System-Dateimanager / Finder anzeigen",
		"d                      Smart Delete (.trash-kompatibel zu Vault Pilot, inkl. Kaskade)",
		"r                      Vault neu scannen",
		"?                      Diese Tastaturhilfe anzeigen",
		"q / Ctrl+C             obdu beenden",
	}
	return "obdu – Tastatursteuerung (ncdu-Stil)", lines
}

func describeNodeKind(n *model.Node) string {
	switch n.Kind {
	case model.KindDir:
		if n.IsHidden {
			return "System-/Dot-Ordner"
		}
		return "Ordner"
	case model.KindNote:
		return fmt.Sprintf("Notiz (.%s)", n.Ext)
	case model.KindAttachment:
		if n.Ext != "" {
			return fmt.Sprintf("Attachment (.%s)", n.Ext)
		}
		return "Attachment"
	default:
		return "Systemdatei"
	}
}

func ternaryStr(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
