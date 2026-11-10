package model

// NodeKind classifies a filesystem node within the vault or directory tree.
type NodeKind uint8

const (
	KindDir        NodeKind = iota
	KindNote                // Active .md, .canvas, .base outside hidden/system dirs
	KindAttachment          // Active non-note files outside hidden/system dirs
	KindSystem              // Any file inside .obsidian, .trash, .git, .agents, node_modules, etc.
)

func (k NodeKind) String() string {
	switch k {
	case KindDir:
		return "dir"
	case KindNote:
		return "note"
	case KindAttachment:
		return "attachment"
	case KindSystem:
		return "system"
	default:
		return "unknown"
	}
}

// SizeMode controls whether referenced local attachments are attributed to owning notes.
type SizeMode string

const (
	SizePhysical SizeMode = "physical"
	SizeLogical  SizeMode = "logical"
)

// BlockMode controls whether apparent file size or allocated 512B/4KiB disk blocks are used.
type BlockMode string

const (
	BlockApparent BlockMode = "apparent"
	BlockDisk     BlockMode = "blocks"
)

// BarStyle controls the ncdu progress/percentage bar column.
type BarStyle uint8

const (
	BarBoth BarStyle = iota
	BarHashOnly
	BarPercentOnly
	BarNone
)

// ViewTab identifies one of the 4 global views (1..4).
type ViewTab uint8

const (
	TabTree ViewTab = iota
	TabOrphans
	TabLargest
	TabIssues
)

// OrphanScope filters the Orphans view (t key in view 2).
type OrphanScope uint8

const (
	OrphanAttachments OrphanScope = iota
	OrphanNotes
	OrphanAll
)

// SortKey controls directory and list ordering.
type SortKey string

const (
	SortBySize  SortKey = "size"
	SortByName  SortKey = "name"
	SortByItems SortKey = "items"
	SortByMTime SortKey = "mtime"
)

// DrillMode represents the current inspection level in the Tree view.
type DrillMode string

const (
	ModeDir        DrillMode = "dir"
	ModeNote       DrillMode = "note"
	ModeAttachment DrillMode = "attachment"
)

// RowRole describes the semantic behavior of a row in the TUI list.
type RowRole string

const (
	RoleParent           RowRole = "parent"
	RoleTreeNode         RowRole = "tree-node"
	RoleNoteSelf         RowRole = "note-self"
	RoleNoteAttachment   RowRole = "note-attachment"
	RoleNoteExternal     RowRole = "note-external"
	RoleNoteOutgoing     RowRole = "note-outgoing"
	RoleNoteBacklink     RowRole = "note-backlink"
	RoleNoteBroken       RowRole = "note-broken"
	RoleAttachmentOwner  RowRole = "attachment-owner"
	RoleDiagnosticBroken RowRole = "diagnostic-broken"
	RoleDiagnosticEmpty  RowRole = "diagnostic-empty"
)

// ExternalRef represents an offloaded Google Drive file or embedded external web asset.
type ExternalRef struct {
	URL      string `json:"url"`
	Label    string `json:"label"`
	Provider string `json:"provider"` // "gdrive" | "web"
}

// ScannedEntry holds raw metadata (and optional text content for active notes) produced by scanner.
type ScannedEntry struct {
	RelPath       string
	AbsPath       string
	Name          string
	IsDir         bool
	ApparentBytes int64
	DiskBytes     int64
	MTimeUnix     int64
	Content       string
	HasContent    bool
}

// Node represents a directory or file in the vault graph.
type Node struct {
	Name          string
	RelPath       string // Forward-slash normalized vault-relative path ("" for Root)
	AbsPath       string
	Ext           string // Lowercase without leading dot
	Kind          NodeKind
	IsHidden      bool
	ApparentBytes int64
	DiskBytes     int64
	MTimeUnix     int64
	Parent        *Node
	Children      []*Node

	OutgoingNotes       []*Node
	IncomingNotes       []*Node
	OutgoingAttachments []*Node
	IncomingNoteOwners  []*Node
	BrokenLinks         []string
	ExternalRefs        []ExternalRef
	IsBookmarked        bool
	EmptyReason         string // "", "0 Bytes", "Nur Leerzeichen", "Nur Frontmatter", "Leerer Ordner"

	DisplayBytes     int64 // Dynamically updated on mode toggles without re-reading disk
	VisibleItemCount int
}

// VaultGraph contains the complete indexed vault hierarchy, reference graph, and summary metrics.
type VaultGraph struct {
	VaultName             string
	VaultRoot             string
	IsVault               bool
	Root                  *Node
	NodesByPath           map[string]*Node
	AllFiles              []*Node
	TotalPhysicalBytes    int64
	TotalLogicalBytes     int64
	OrphanAttachmentCount int
	OrphanAttachmentBytes int64
}

// DisplayRow represents a single rendered line in the TUI table.
type DisplayRow struct {
	Key           string
	Role          RowRole
	Flag          rune
	Bytes         int64
	ItemCount     int
	ShowBar       bool
	Label         string
	Badge         string
	Node          *Node
	ExternalRef   *ExternalRef
	BrokenTarget  string
	BrokenSources []*Node
}

// NavFrame records a point in the multi-hop navigation history stack.
type NavFrame struct {
	Mode           DrillMode
	TargetPath     string
	SelectedRowKey string
	TopOffset      int
}

// DeletePlan summarizes the impact of deleting a node or directory subtree.
type DeletePlan struct {
	Target                          *Node
	IsInsideTrash                   bool
	ExclusiveAttachments            []*Node
	SharedAttachments               []*Node
	IncomingBacklinks               []*Node
	ReclaimableBytesTargetOnly      int64
	ReclaimableBytesWithAttachments int64
}

// DeleteOptions configures how ExecuteDelete performs the deletion.
type DeleteOptions struct {
	Permanent                   bool
	IncludeExclusiveAttachments bool
}
