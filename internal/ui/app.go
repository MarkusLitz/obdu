package ui

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/MarkusLitz/obdu/internal/actions"
	"github.com/MarkusLitz/obdu/internal/graph"
	"github.com/MarkusLitz/obdu/internal/model"
	"github.com/MarkusLitz/obdu/internal/parser"
	"github.com/MarkusLitz/obdu/internal/scanner"
	"github.com/gdamore/tcell/v2"
)

const Version = "0.1.0"

const maxPreviewTextBytes = 512 * 1024

var previewableTextExts = map[string]bool{
	"md":         true,
	"txt":        true,
	"canvas":     true,
	"base":       true,
	"json":       true,
	"yaml":       true,
	"yml":        true,
	"css":        true,
	"js":         true,
	"ts":         true,
	"mjs":        true,
	"csv":        true,
	"xml":        true,
	"html":       true,
	"sh":         true,
	"py":         true,
	"go":         true,
	"toml":       true,
	"ini":        true,
	"conf":       true,
	"gitignore":  true,
	"dockerfile": true,
}

type modalKind uint8

const (
	modalNone modalKind = iota
	modalInfo
	modalPreview
	modalHelp
	modalDelete
)

// Config holds initial CLI state for launching the interactive TUI.
type Config struct {
	VaultRoot      string
	VaultName      string
	InitialRelPath string
	IsVault        bool
	SizeMode       model.SizeMode
	BlockMode      model.BlockMode
	ShowHidden     bool
}

// App manages the tcell screen, navigation stack, modals, and user interaction.
type App struct {
	cfg           Config
	screen        tcell.Screen
	graph         *model.VaultGraph
	viewTab       model.ViewTab
	orphanScope   model.OrphanScope
	sizeMode      model.SizeMode
	blockMode     model.BlockMode
	showHidden    bool
	showCounts    bool
	barStyle      model.BarStyle
	sortKey       model.SortKey
	sortAsc       bool
	currentMode   model.DrillMode
	currentPath   string
	navStack      []model.NavFrame
	filterActive  bool
	filterQuery   string
	rows          []model.DisplayRow
	selectedIndex int
	topOffset     int
	statusMsg     string

	activeModal modalKind
	modalTitle  string
	modalLines  []string
	modalScroll int
	deletePlan  *model.DeletePlan
	deleteOpts  model.DeleteOptions
}

// NewApp creates a new TUI application instance initialized at Config.InitialRelPath.
func NewApp(cfg Config, initialGraph *model.VaultGraph) *App {
	app := &App{
		cfg:         cfg,
		graph:       initialGraph,
		viewTab:     model.TabTree,
		orphanScope: model.OrphanAttachments,
		sizeMode:    cfg.SizeMode,
		blockMode:   cfg.BlockMode,
		showHidden:  cfg.ShowHidden,
		barStyle:    model.BarBoth,
		sortKey:     model.SortBySize,
		sortAsc:     false,
		currentMode: model.ModeDir,
		currentPath: cfg.InitialRelPath,
	}
	app.recalculateSizes()
	app.rebuildRows("")
	return app
}

// Run initializes the tcell alternate screen and runs the main event loop until quit.
func (a *App) Run() error {
	s, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := s.Init(); err != nil {
		return err
	}
	defer s.Fini()

	a.screen = s
	a.render()

	for {
		ev := s.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventResize:
			s.Sync()
			a.render()
		case *tcell.EventKey:
			if a.handleKeyEvent(ev) {
				return nil
			}
			a.render()
		}
	}
}

func (a *App) recalculateSizes() {
	if a.graph == nil {
		return
	}
	graph.RecomputeDisplaySizes(a.graph.Root, a.graph.AllFiles, a.sizeMode, a.blockMode, a.showHidden)
}

func (a *App) rebuildRows(preferredKey string) {
	a.rows = a.computeRows()
	if preferredKey != "" {
		for idx, r := range a.rows {
			if r.Key == preferredKey {
				a.selectedIndex = idx
				break
			}
		}
	}
	if a.selectedIndex >= len(a.rows) {
		a.selectedIndex = len(a.rows) - 1
	}
	if a.selectedIndex < 0 {
		a.selectedIndex = 0
	}
}

func (a *App) handleKeyEvent(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyCtrlC {
		return true
	}

	// Clear transient footer status message on next keypress
	a.statusMsg = ""

	if a.activeModal != modalNone {
		a.handleModalKey(ev)
		return false
	}

	if a.filterActive {
		a.handleFilterKey(ev)
		return false
	}

	switch ev.Key() {
	case tcell.KeyUp:
		a.moveSelection(-1)
		return false
	case tcell.KeyDown:
		a.moveSelection(1)
		return false
	case tcell.KeyPgUp:
		a.moveSelection(-15)
		return false
	case tcell.KeyPgDn:
		a.moveSelection(15)
		return false
	case tcell.KeyHome:
		a.moveSelection(-len(a.rows))
		return false
	case tcell.KeyEnd:
		a.moveSelection(len(a.rows))
		return false
	case tcell.KeyEnter, tcell.KeyRight:
		a.activateSelectedRow()
		return false
	case tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyLeft:
		a.navigateBack()
		return false
	case tcell.KeyTab:
		a.toggleSizeMode()
		return false
	case tcell.KeyEscape:
		if a.filterQuery != "" {
			a.filterQuery = ""
			a.rebuildRows("")
		}
		return false
	case tcell.KeyRune:
		r := ev.Rune()
		switch r {
		case 'q':
			return true
		case 'j':
			a.moveSelection(1)
		case 'k':
			a.moveSelection(-1)
		case 'l':
			a.activateSelectedRow()
		case 'h':
			a.navigateBack()
		case 'm':
			a.toggleSizeMode()
		case 'a':
			a.toggleBlockMode()
		case '.', 'e':
			a.toggleHidden()
		case 'c':
			a.showCounts = !a.showCounts
		case 'g':
			a.barStyle = (a.barStyle + 1) % 4
		case 's':
			a.setSort(model.SortBySize)
		case 'n':
			a.setSort(model.SortByName)
		case 'C':
			a.setSort(model.SortByItems)
		case 'M':
			a.setSort(model.SortByMTime)
		case '1':
			a.switchTab(model.TabTree)
		case '2':
			a.switchTab(model.TabOrphans)
		case '3':
			a.switchTab(model.TabLargest)
		case '4':
			a.switchTab(model.TabIssues)
		case 't':
			if a.viewTab == model.TabOrphans {
				a.orphanScope = (a.orphanScope + 1) % 3
				a.selectedIndex = 0
				a.topOffset = 0
				a.rebuildRows("")
			}
		case '/':
			a.filterActive = true
		case 'i':
			a.openInfoModal()
		case 'p', ' ':
			a.openPreview()
		case 'o':
			a.openInObsidianOrBrowser()
		case 'f':
			a.revealInFileManager()
		case 'd':
			a.openDeleteModal()
		case 'r':
			a.rescanVault("")
		case '?':
			a.modalTitle, a.modalLines = buildHelpModalLines()
			a.modalScroll = 0
			a.activeModal = modalHelp
		}
	}
	return false
}

func (a *App) handleFilterKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEnter, tcell.KeyDown, tcell.KeyUp:
		a.filterActive = false
	case tcell.KeyEscape:
		a.filterActive = false
		a.filterQuery = ""
		a.selectedIndex = 0
		a.topOffset = 0
		a.rebuildRows("")
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(a.filterQuery) > 0 {
			_, size := utf8.DecodeLastRuneInString(a.filterQuery)
			a.filterQuery = a.filterQuery[:len(a.filterQuery)-size]
			a.selectedIndex = 0
			a.topOffset = 0
			a.rebuildRows("")
		}
	case tcell.KeyRune:
		r := ev.Rune()
		a.filterQuery += string(r)
		a.selectedIndex = 0
		a.topOffset = 0
		a.rebuildRows("")
	}
}

func (a *App) handleModalKey(ev *tcell.EventKey) {
	if a.activeModal == modalDelete {
		switch ev.Key() {
		case tcell.KeyEscape:
			a.activeModal = modalNone
			a.deletePlan = nil
			return
		case tcell.KeyEnter:
			a.confirmDelete()
			return
		case tcell.KeyUp:
			a.modalScroll--
			return
		case tcell.KeyDown:
			a.modalScroll++
			return
		case tcell.KeyRune:
			switch ev.Rune() {
			case 'y', 'Y':
				a.confirmDelete()
				return
			case 'n', 'N', 'q':
				a.activeModal = modalNone
				a.deletePlan = nil
				return
			case 'a', 'A':
				if a.deletePlan != nil && len(a.deletePlan.ExclusiveAttachments) > 0 {
					a.deleteOpts.IncludeExclusiveAttachments = !a.deleteOpts.IncludeExclusiveAttachments
					a.modalTitle, a.modalLines = a.buildDeleteModalLines()
				}
				return
			case 'x', 'X':
				if a.deletePlan != nil && !a.deletePlan.IsInsideTrash && a.graph != nil && a.graph.IsVault {
					a.deleteOpts.Permanent = !a.deleteOpts.Permanent
					a.modalTitle, a.modalLines = a.buildDeleteModalLines()
				}
				return
			}
		}
		return
	}

	switch ev.Key() {
	case tcell.KeyEscape:
		a.activeModal = modalNone
	case tcell.KeyUp:
		a.modalScroll--
	case tcell.KeyDown:
		a.modalScroll++
	case tcell.KeyPgUp:
		a.modalScroll -= 10
	case tcell.KeyPgDn:
		a.modalScroll += 10
	case tcell.KeyHome:
		a.modalScroll = 0
	case tcell.KeyEnd:
		a.modalScroll = len(a.modalLines)
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q', 'i', '?', 'p', ' ':
			a.activeModal = modalNone
		case 'j':
			a.modalScroll++
		case 'k':
			a.modalScroll--
		}
	}
}

func (a *App) moveSelection(delta int) {
	if len(a.rows) == 0 {
		return
	}
	next := a.selectedIndex + delta
	if next < 0 {
		next = 0
	}
	if next >= len(a.rows) {
		next = len(a.rows) - 1
	}
	a.selectedIndex = next
}

func (a *App) switchTab(tab model.ViewTab) {
	a.viewTab = tab
	a.selectedIndex = 0
	a.topOffset = 0
	a.rebuildRows("")
}

func (a *App) currentSelectedKey() string {
	if a.selectedIndex >= 0 && a.selectedIndex < len(a.rows) {
		return a.rows[a.selectedIndex].Key
	}
	return ""
}

func (a *App) toggleSizeMode() {
	keep := a.currentSelectedKey()
	if a.sizeMode == model.SizePhysical {
		a.sizeMode = model.SizeLogical
	} else {
		a.sizeMode = model.SizePhysical
	}
	a.recalculateSizes()
	a.rebuildRows(keep)
}

func (a *App) toggleBlockMode() {
	keep := a.currentSelectedKey()
	if a.blockMode == model.BlockApparent {
		a.blockMode = model.BlockDisk
	} else {
		a.blockMode = model.BlockApparent
	}
	a.recalculateSizes()
	a.rebuildRows(keep)
}

func (a *App) toggleHidden() {
	keep := a.currentSelectedKey()
	a.showHidden = !a.showHidden
	a.recalculateSizes()
	a.rebuildRows(keep)
}

func (a *App) setSort(key model.SortKey) {
	keep := a.currentSelectedKey()
	if a.sortKey == key {
		a.sortAsc = !a.sortAsc
	} else {
		a.sortKey = key
		a.sortAsc = key == model.SortByName
	}
	a.rebuildRows(keep)
}

func (a *App) activateSelectedRow() {
	if a.selectedIndex < 0 || a.selectedIndex >= len(a.rows) {
		return
	}
	row := a.rows[a.selectedIndex]
	if row.Role == model.RoleParent {
		a.navigateBack()
		return
	}
	if row.Role == model.RoleNoteExternal && row.ExternalRef != nil {
		if err := actions.OpenExternalURL(row.ExternalRef.URL); err != nil {
			a.statusMsg = "Fehler beim Öffnen der URL: " + err.Error()
		} else {
			a.statusMsg = "Externe URL im Browser geöffnet."
		}
		return
	}
	if row.Role == model.RoleNoteSelf && row.Node != nil {
		a.openPreviewForNode(row.Node)
		return
	}
	if row.Node == nil {
		return
	}

	if a.viewTab == model.TabTree {
		a.navStack = append(a.navStack, model.NavFrame{
			Mode:           a.currentMode,
			TargetPath:     a.currentPath,
			SelectedRowKey: row.Key,
			TopOffset:      a.topOffset,
		})
	} else {
		parentPath := ""
		if row.Node.Parent != nil {
			parentPath = row.Node.Parent.RelPath
		}
		a.viewTab = model.TabTree
		a.navStack = []model.NavFrame{
			{
				Mode:           model.ModeDir,
				TargetPath:     parentPath,
				SelectedRowKey: "node:" + row.Node.RelPath,
				TopOffset:      0,
			},
		}
	}

	switch row.Node.Kind {
	case model.KindDir:
		a.currentMode = model.ModeDir
		a.currentPath = row.Node.RelPath
	case model.KindNote:
		a.currentMode = model.ModeNote
		a.currentPath = row.Node.RelPath
	case model.KindAttachment:
		if a.graph != nil && a.graph.IsVault {
			a.currentMode = model.ModeAttachment
			a.currentPath = row.Node.RelPath
		} else {
			a.navStack = a.navStack[:len(a.navStack)-1]
			a.openPreviewForNode(row.Node)
			return
		}
	default:
		a.navStack = a.navStack[:len(a.navStack)-1]
		a.openPreviewForNode(row.Node)
		return
	}

	a.filterQuery = ""
	a.filterActive = false
	a.selectedIndex = 0
	a.topOffset = 0
	a.rebuildRows("")
}

func (a *App) navigateBack() {
	if a.viewTab != model.TabTree {
		a.viewTab = model.TabTree
		a.selectedIndex = 0
		a.topOffset = 0
		a.rebuildRows("")
		return
	}
	if len(a.navStack) > 0 {
		prev := a.navStack[len(a.navStack)-1]
		a.navStack = a.navStack[:len(a.navStack)-1]
		a.currentMode = prev.Mode
		a.currentPath = prev.TargetPath
		a.topOffset = prev.TopOffset
		a.rebuildRows(prev.SelectedRowKey)
		return
	}
	if a.currentPath != "" && a.graph != nil {
		curNode := a.graph.NodesByPath[a.currentPath]
		parentPath := ""
		prevKey := ""
		if curNode != nil {
			if curNode.Parent != nil {
				parentPath = curNode.Parent.RelPath
			}
			prevKey = "node:" + curNode.RelPath
		}
		a.currentMode = model.ModeDir
		a.currentPath = parentPath
		a.topOffset = 0
		a.rebuildRows(prevKey)
	}
}

func (a *App) selectedNode() *model.Node {
	if a.selectedIndex >= 0 && a.selectedIndex < len(a.rows) {
		return a.rows[a.selectedIndex].Node
	}
	return nil
}

func (a *App) openInfoModal() {
	node := a.selectedNode()
	if node == nil {
		return
	}
	a.modalTitle, a.modalLines = a.buildInfoModalLines(node)
	a.modalScroll = 0
	a.activeModal = modalInfo
}

func (a *App) openPreview() {
	node := a.selectedNode()
	if node == nil {
		return
	}
	a.openPreviewForNode(node)
}

func (a *App) openPreviewForNode(node *model.Node) {
	if node.Kind == model.KindDir {
		a.modalTitle = "Vorschau: /" + parser.SanitizeDisplayString(node.RelPath)
		a.modalLines = []string{
			fmt.Sprintf("Ordner mit %d direkten Einträgen (%s).", len(node.Children), graph.FormatBytesNcdu(node.DisplayBytes)),
		}
		a.modalScroll = 0
		a.activeModal = modalPreview
		return
	}

	if previewableTextExts[node.Ext] && node.ApparentBytes <= maxPreviewTextBytes {
		safeAbs, err := actions.EnsureInsideRoot(a.cfg.VaultRoot, node.RelPath)
		if err != nil {
			a.statusMsg = "Vorschau verweigert: " + err.Error()
			return
		}
		raw, err := os.ReadFile(safeAbs)
		if err != nil {
			a.statusMsg = "Datei konnte nicht gelesen werden: " + err.Error()
			return
		}
		rawLines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
		if len(rawLines) > 400 {
			rawLines = append(rawLines[:400], "… [Vorschau nach 400 Zeilen gekürzt]")
		}
		lines := make([]string, len(rawLines))
		for i, l := range rawLines {
			lines[i] = parser.SanitizeDisplayString(l)
		}
		if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
			lines = []string{"(Leere Datei)"}
		}
		a.modalTitle = fmt.Sprintf("Textvorschau: %s (%s)", parser.SanitizeDisplayString(node.Name), graph.FormatBytesNcdu(node.ApparentBytes))
		a.modalLines = lines
		a.modalScroll = 0
		a.activeModal = modalPreview
		return
	}

	if err := actions.PreviewExternal(a.cfg.VaultRoot, node.RelPath); err != nil {
		a.statusMsg = "Externe Vorschau fehlgeschlagen: " + err.Error()
	} else {
		a.statusMsg = "Vorschau geöffnet: " + parser.SanitizeDisplayString(node.Name)
	}
}

func (a *App) openInObsidianOrBrowser() {
	if a.selectedIndex >= 0 && a.selectedIndex < len(a.rows) {
		row := a.rows[a.selectedIndex]
		if row.ExternalRef != nil {
			if err := actions.OpenExternalURL(row.ExternalRef.URL); err != nil {
				a.statusMsg = "Fehler: " + err.Error()
			} else {
				a.statusMsg = "Im Browser geöffnet."
			}
			return
		}
	}
	node := a.selectedNode()
	if node == nil || node.RelPath == "" {
		return
	}
	if err := actions.OpenInObsidian(a.cfg.VaultName, a.cfg.VaultRoot, node.RelPath); err != nil {
		a.statusMsg = "Obsidian-Start fehlgeschlagen: " + err.Error()
	} else {
		a.statusMsg = "In Obsidian geöffnet: " + parser.SanitizeDisplayString(node.Name)
	}
}

func (a *App) revealInFileManager() {
	node := a.selectedNode()
	if node == nil || node.RelPath == "" {
		return
	}
	if err := actions.RevealInFileManager(a.cfg.VaultRoot, node.RelPath); err != nil {
		a.statusMsg = "Dateimanager-Aufruf fehlgeschlagen: " + err.Error()
	} else {
		a.statusMsg = "Im Dateimanager angezeigt: " + parser.SanitizeDisplayString(node.RelPath)
	}
}

func (a *App) openDeleteModal() {
	node := a.selectedNode()
	if node == nil || node.RelPath == "" {
		return
	}
	if _, err := actions.EnsureInsideRoot(a.cfg.VaultRoot, node.RelPath); err != nil {
		a.statusMsg = "Geschützter Pfad: " + err.Error()
		return
	}
	plan := actions.BuildDeletePlan(node)
	a.deletePlan = plan
	a.deleteOpts = model.DeleteOptions{
		Permanent:                   !a.cfg.IsVault || plan.IsInsideTrash,
		IncludeExclusiveAttachments: len(plan.ExclusiveAttachments) > 0,
	}
	a.modalTitle, a.modalLines = a.buildDeleteModalLines()
	a.modalScroll = 0
	a.activeModal = modalDelete
}

func (a *App) confirmDelete() {
	plan := a.deletePlan
	opts := a.deleteOpts
	a.activeModal = modalNone
	a.deletePlan = nil
	if plan == nil || plan.Target == nil {
		return
	}

	nextKey := ""
	if a.selectedIndex+1 < len(a.rows) {
		nextKey = a.rows[a.selectedIndex+1].Key
	} else if a.selectedIndex-1 >= 0 {
		nextKey = a.rows[a.selectedIndex-1].Key
	}

	if err := actions.ExecuteDelete(a.cfg.VaultRoot, a.cfg.IsVault, plan, opts); err != nil {
		a.statusMsg = "Löschen fehlgeschlagen: " + err.Error()
		return
	}

	if a.currentPath == plan.Target.RelPath {
		a.navigateBack()
	}
	a.rescanVault(nextKey)
	if opts.Permanent || plan.IsInsideTrash || !a.cfg.IsVault {
		a.statusMsg = fmt.Sprintf("„%s“ endgültig gelöscht.", parser.SanitizeDisplayString(plan.Target.Name))
	} else {
		a.statusMsg = fmt.Sprintf("„%s“ in .trash/ verschoben.", parser.SanitizeDisplayString(plan.Target.Name))
	}
}

func (a *App) rescanVault(preserveKey string) {
	if preserveKey == "" {
		preserveKey = a.currentSelectedKey()
	}
	entries, err := scanner.ScanRoot(a.cfg.VaultRoot)
	if err != nil {
		a.statusMsg = "Scan-Fehler: " + err.Error()
		return
	}
	var bookmarks map[string]bool
	if a.cfg.IsVault {
		bookmarks = scanner.LoadBookmarkedPaths(a.cfg.VaultRoot)
	}
	a.graph = graph.BuildVaultGraph(a.cfg.VaultName, a.cfg.VaultRoot, a.cfg.IsVault, entries, bookmarks)
	if a.currentPath != "" && a.graph.NodesByPath[a.currentPath] == nil {
		a.currentMode = model.ModeDir
		a.currentPath = ""
		a.navStack = nil
	}
	a.recalculateSizes()
	a.rebuildRows(preserveKey)
	if a.statusMsg == "" {
		a.statusMsg = "Vault erfolgreich neu gescannt."
	}
}
