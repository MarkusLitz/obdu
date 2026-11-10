package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MarkusLitz/obdu/internal/graph"
	"github.com/MarkusLitz/obdu/internal/model"
	"github.com/MarkusLitz/obdu/internal/parser"
	"github.com/MarkusLitz/obdu/internal/scanner"
	"github.com/MarkusLitz/obdu/internal/ui"
)

type cliOptions struct {
	logical       bool
	excludeHidden bool
	blocks        bool
	summary       bool
	jsonOutput    bool
	version       bool
	help          bool
	targetPath    string
}

func parseCLIArgs(args []string) (cliOptions, error) {
	opts := cliOptions{
		targetPath: ".",
	}
	var posArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			posArgs = append(posArgs, args[i+1:]...)
			break
		}
		switch arg {
		case "-l", "--logical":
			opts.logical = true
		case "-x", "--exclude-hidden":
			opts.excludeHidden = true
		case "-lx", "-xl":
			opts.logical = true
			opts.excludeHidden = true
		case "--blocks":
			opts.blocks = true
		case "--summary":
			opts.summary = true
		case "--json":
			opts.jsonOutput = true
		case "-v", "--version":
			opts.version = true
		case "-h", "--help":
			opts.help = true
		default:
			if strings.HasPrefix(arg, "-") {
				return opts, fmt.Errorf("unknown flag: %s", arg)
			}
			posArgs = append(posArgs, arg)
		}
	}
	if len(posArgs) > 0 {
		opts.targetPath = posArgs[0]
	}
	return opts, nil
}

func printUsage() {
	fmt.Print(`Usage:
  obdu [flags] [path]

Flags:
  -l, --logical          Start in Logical size mode (attribute referenced attachments to notes)
  -x, --exclude-hidden   Start with dot/system folders (.obsidian, .trash, .git, etc.) hidden
      --blocks           Start in filesystem Disk Block mode (st_blocks * 512) instead of Apparent Bytes
      --summary          Print a non-interactive directory & orphan summary to stdout and exit
      --json             Export full vault size & reference report as JSON to stdout and exit
      --version          Print obdu version and exit
  -h, --help             Show this help message
`)
}

type jsonNodeReport struct {
	Name                string              `json:"name"`
	RelPath             string              `json:"relPath"`
	Kind                string              `json:"kind"`
	IsHidden            bool                `json:"isHidden"`
	ApparentBytes       int64               `json:"apparentBytes"`
	DiskBytes           int64               `json:"diskBytes"`
	DisplayBytes        int64               `json:"displayBytes"`
	VisibleItemCount    int                 `json:"visibleItemCount"`
	IsBookmarked        bool                `json:"isBookmarked,omitempty"`
	EmptyReason         string              `json:"emptyReason,omitempty"`
	OutgoingNotes       []string            `json:"outgoingNotes,omitempty"`
	IncomingNotes       []string            `json:"incomingNotes,omitempty"`
	OutgoingAttachments []string            `json:"outgoingAttachments,omitempty"`
	IncomingNoteOwners  []string            `json:"incomingNoteOwners,omitempty"`
	BrokenLinks         []string            `json:"brokenLinks,omitempty"`
	ExternalRefs        []model.ExternalRef `json:"externalRefs,omitempty"`
}

type jsonVaultReport struct {
	Version               string           `json:"version"`
	VaultName             string           `json:"vaultName"`
	VaultRoot             string           `json:"vaultRoot"`
	InitialRelPath        string           `json:"initialRelPath"`
	IsVault               bool             `json:"isVault"`
	SizeMode              model.SizeMode   `json:"sizeMode"`
	BlockMode             model.BlockMode  `json:"blockMode"`
	ShowHidden            bool             `json:"showHidden"`
	TotalDisplayBytes     int64            `json:"totalDisplayBytes"`
	TotalPhysicalBytes    int64            `json:"totalPhysicalBytes"`
	TotalLogicalBytes     int64            `json:"totalLogicalBytes"`
	OrphanAttachmentCount int              `json:"orphanAttachmentCount"`
	OrphanAttachmentBytes int64            `json:"orphanAttachmentBytes"`
	Entries               []jsonNodeReport `json:"entries"`
}

func relPathsOf(nodes []*model.Node) []string {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.RelPath
	}
	return out
}

func printJSONReport(
	vg *model.VaultGraph,
	initRel string,
	sizeMode model.SizeMode,
	blockMode model.BlockMode,
	showHidden bool,
) error {
	report := jsonVaultReport{
		Version:               ui.Version,
		VaultName:             vg.VaultName,
		VaultRoot:             vg.VaultRoot,
		InitialRelPath:        initRel,
		IsVault:               vg.IsVault,
		SizeMode:              sizeMode,
		BlockMode:             blockMode,
		ShowHidden:            showHidden,
		TotalDisplayBytes:     vg.Root.DisplayBytes,
		TotalPhysicalBytes:    vg.TotalPhysicalBytes,
		TotalLogicalBytes:     vg.TotalLogicalBytes,
		OrphanAttachmentCount: vg.OrphanAttachmentCount,
		OrphanAttachmentBytes: vg.OrphanAttachmentBytes,
	}

	var walk func(n *model.Node)
	walk = func(n *model.Node) {
		if !showHidden && n.IsHidden {
			return
		}
		if n.RelPath != "" {
			report.Entries = append(report.Entries, jsonNodeReport{
				Name:                n.Name,
				RelPath:             n.RelPath,
				Kind:                n.Kind.String(),
				IsHidden:            n.IsHidden,
				ApparentBytes:       n.ApparentBytes,
				DiskBytes:           n.DiskBytes,
				DisplayBytes:        n.DisplayBytes,
				VisibleItemCount:    n.VisibleItemCount,
				IsBookmarked:        n.IsBookmarked,
				EmptyReason:         n.EmptyReason,
				OutgoingNotes:       relPathsOf(n.OutgoingNotes),
				IncomingNotes:       relPathsOf(n.IncomingNotes),
				OutgoingAttachments: relPathsOf(n.OutgoingAttachments),
				IncomingNoteOwners:  relPathsOf(n.IncomingNoteOwners),
				BrokenLinks:         n.BrokenLinks,
				ExternalRefs:        n.ExternalRefs,
			})
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(vg.Root)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func printSummary(
	vg *model.VaultGraph,
	initRel string,
	sizeMode model.SizeMode,
	blockMode model.BlockMode,
	showHidden bool,
) {
	target := vg.NodesByPath[initRel]
	if target == nil {
		target = vg.Root
	}

	fmt.Printf("obdu %s — Obsidian Disk Usage Summary\n", ui.Version)
	fmt.Printf("Vault Root:     %s (isVault=%v)\n", parser.SanitizeDisplayString(vg.VaultRoot), vg.IsVault)
	if initRel != "" {
		fmt.Printf("Start Subpath:  /%s\n", parser.SanitizeDisplayString(initRel))
	}
	fmt.Printf("Mode:           size=%s | blocks=%s | showHidden=%v\n", sizeMode, blockMode, showHidden)
	fmt.Printf("Vault Total:    %s (%d B)\n", graph.FormatBytesNcdu(vg.Root.DisplayBytes), vg.Root.DisplayBytes)
	if vg.IsVault {
		fmt.Printf("Orphans:        %d attachment(s) (%s / %d B)\n",
			vg.OrphanAttachmentCount,
			graph.FormatBytesNcdu(vg.OrphanAttachmentBytes),
			vg.OrphanAttachmentBytes,
		)
	}
	fmt.Println(strings.Repeat("-", 78))

	children := make([]*model.Node, 0, len(target.Children))
	for _, c := range target.Children {
		if showHidden || !c.IsHidden {
			children = append(children, c)
		}
	}
	sort.Slice(children, func(i, j int) bool {
		if children[i].DisplayBytes != children[j].DisplayBytes {
			return children[i].DisplayBytes > children[j].DisplayBytes
		}
		return children[i].RelPath < children[j].RelPath
	})

	for _, c := range children {
		flag := graph.GetNodeFlag(c, vg.IsVault)
		prefix := ""
		if c.Kind == model.KindDir {
			prefix = "/"
		}
		badge := graph.GetNodeBadge(c, sizeMode, vg.IsVault)
		name := prefix + parser.SanitizeDisplayString(c.Name)
		if badge != "" {
			fmt.Printf(" %c %10s  (%8d B)  %-38s  %s\n",
				flag,
				graph.FormatBytesNcdu(c.DisplayBytes),
				c.DisplayBytes,
				name,
				parser.SanitizeDisplayString(badge),
			)
		} else {
			fmt.Printf(" %c %10s  (%8d B)  %s\n",
				flag,
				graph.FormatBytesNcdu(c.DisplayBytes),
				c.DisplayBytes,
				name,
			)
		}
	}
}

func main() {
	opts, err := parseCLIArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "obdu: %v\n\n", err)
		printUsage()
		os.Exit(2)
	}
	if opts.help {
		printUsage()
		return
	}
	if opts.version {
		fmt.Printf("obdu %s\n", ui.Version)
		return
	}

	vaultRoot, initRelPath, isVault, err := scanner.FindVaultRoot(opts.targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "obdu: failed to resolve path %q: %v\n", opts.targetPath, err)
		os.Exit(1)
	}

	vaultName := filepath.Base(vaultRoot)
	entries, err := scanner.ScanRoot(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "obdu: failed to scan %q: %v\n", vaultRoot, err)
		os.Exit(1)
	}

	var bookmarks map[string]bool
	if isVault {
		bookmarks = scanner.LoadBookmarkedPaths(vaultRoot)
	}

	vg := graph.BuildVaultGraph(vaultName, vaultRoot, isVault, entries, bookmarks)

	sizeMode := model.SizePhysical
	if opts.logical {
		sizeMode = model.SizeLogical
	}
	blockMode := model.BlockApparent
	if opts.blocks {
		blockMode = model.BlockDisk
	}
	showHidden := !opts.excludeHidden

	graph.RecomputeDisplaySizes(vg.Root, vg.AllFiles, sizeMode, blockMode, showHidden)

	if opts.jsonOutput {
		if err := printJSONReport(vg, initRelPath, sizeMode, blockMode, showHidden); err != nil {
			fmt.Fprintf(os.Stderr, "obdu: json export failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if opts.summary {
		printSummary(vg, initRelPath, sizeMode, blockMode, showHidden)
		return
	}

	app := ui.NewApp(ui.Config{
		VaultRoot:      vaultRoot,
		VaultName:      vaultName,
		InitialRelPath: initRelPath,
		IsVault:        isVault,
		SizeMode:       sizeMode,
		BlockMode:      blockMode,
		ShowHidden:     showHidden,
	}, vg)

	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "obdu: TUI error: %v\n", err)
		os.Exit(1)
	}
}
