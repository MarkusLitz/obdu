package ui

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/MarkusLitz/obdu/internal/graph"
	"github.com/MarkusLitz/obdu/internal/model"
	"github.com/gdamore/tcell/v2"
)

type dumpedCell struct {
	Ch   string `json:"ch"`
	FG   string `json:"fg"`
	BG   string `json:"bg"`
	Bold bool   `json:"bold"`
	Dim  bool   `json:"dim"`
}

type dumpedScreen struct {
	Width  int            `json:"width"`
	Height int            `json:"height"`
	Rows   [][]dumpedCell `json:"rows"`
}

func colorName(c tcell.Color, isBG bool) string {
	switch c {
	case tcell.ColorDefault:
		if isBG {
			return "default_bg"
		}
		return "default_fg"
	case tcell.ColorBlack:
		return "black"
	case tcell.ColorWhite:
		return "white"
	case tcell.ColorTeal:
		return "teal"
	case tcell.ColorYellow:
		return "yellow"
	case tcell.ColorRed:
		return "red"
	case tcell.ColorGreen:
		return "green"
	case tcell.ColorAqua:
		return "aqua"
	case tcell.ColorDodgerBlue:
		return "dodgerblue"
	case tcell.Color235:
		return "c235"
	case tcell.Color236:
		return "c236"
	default:
		return "default_fg"
	}
}

func dumpSimScreen(sim tcell.SimulationScreen, outPath string) error {
	cells, w, h := sim.GetContents()
	rows := make([][]dumpedCell, h)
	for y := 0; y < h; y++ {
		row := make([]dumpedCell, w)
		for x := 0; x < w; x++ {
			cell := cells[y*w+x]
			ch := " "
			if len(cell.Runes) > 0 && cell.Runes[0] != 0 {
				ch = string(cell.Runes[0])
			}
			fg, bg, attr := cell.Style.Decompose()
			row[x] = dumpedCell{
				Ch:   ch,
				FG:   colorName(fg, false),
				BG:   colorName(bg, true),
				Bold: attr&tcell.AttrBold != 0,
				Dim:  attr&tcell.AttrDim != 0,
			}
		}
		rows[y] = row
	}
	data, err := json.Marshal(dumpedScreen{
		Width:  w,
		Height: h,
		Rows:   rows,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, data, 0o644)
}

func buildDemoVaultGraph() *model.VaultGraph {
	entries := []model.ScannedEntry{
		{RelPath: "Projects", Name: "Projects", IsDir: true},
		{RelPath: "Engineering", Name: "Engineering", IsDir: true},
		{RelPath: "Journal", Name: "Journal", IsDir: true},
		{RelPath: "Attachments", Name: "Attachments", IsDir: true},
		{RelPath: "Archive", Name: "Archive", IsDir: true},
		{RelPath: ".obsidian", Name: ".obsidian", IsDir: true},
		{RelPath: ".trash", Name: ".trash", IsDir: true},

		// Notes inside /Projects (where the demo screenshot opens)
		{
			RelPath:       "Projects/System Architecture Overview.md",
			Name:          "System Architecture Overview.md",
			ApparentBytes: 1840,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "Diagram: ![[arch-topology-2026.pdf]] and broken ref [[LegacyClusterSpec.pdf]]",
		},
		{
			RelPath:       "Projects/Kubernetes Migration Guide.md",
			Name:          "Kubernetes Migration Guide.md",
			ApparentBytes: 2420,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "Runbook: ![[cluster-rollout-plan.png]] and [📁 K8s-Training-Video.mp4](https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUv123456/view)",
		},
		{
			RelPath:       "Projects/Microservices Latency Study.md",
			Name:          "Microservices Latency Study.md",
			ApparentBytes: 1510,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "Benchmarks: ![[p99-flamegraph.pdf]] and ![[shared-company-logo.svg]] plus [[Engineering/ServiceMesh]]",
		},
		{
			RelPath:       "Projects/API Security Audit Report.md",
			Name:          "API Security Audit Report.md",
			ApparentBytes: 3120,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "Findings: ![[pentest-summary.pdf]] and ![[shared-company-logo.svg]]",
		},
		{
			RelPath:       "Projects/Q4 Product Roadmap (2026).md",
			Name:          "Q4 Product Roadmap (2026).md",
			ApparentBytes: 4120,
			DiskBytes:     8192,
			HasContent:    true,
			Content:       "Quarterly milestones and deliverables.",
		},
		{
			RelPath:       "Projects/Recurring Sprint Tasks.md",
			Name:          "Recurring Sprint Tasks.md",
			ApparentBytes: 1140,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "---\ntitle: Recurring Sprint Tasks\ntasks:\n  - id: 1\n    text: Verify backups\n---\n",
		},
		{
			RelPath:       "Projects/Empty Draft.md",
			Name:          "Empty Draft.md",
			ApparentBytes: 110,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "   \n  \n",
		},
		{
			RelPath:       "Projects/unreferenced-wireframe-v1.png",
			Name:          "unreferenced-wireframe-v1.png",
			ApparentBytes: 63898,
			DiskBytes:     65536,
		},
		{
			RelPath:       "Projects/shared-company-logo.svg",
			Name:          "shared-company-logo.svg",
			ApparentBytes: 18432,
			DiskBytes:     20480,
		},

		// Other vault notes & attachments
		{
			RelPath:       "Engineering/ServiceMesh.md",
			Name:          "ServiceMesh.md",
			ApparentBytes: 2150,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "![[mesh-diagram.png|492]] and ![[shared-company-logo.svg]]",
		},
		{
			RelPath:       "Journal/2026-11.md",
			Name:          "2026-11.md",
			ApparentBytes: 1200,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "![[shared-company-logo.svg]]",
		},
		{
			RelPath:       "Attachments/SystemArch/arch-topology-2026.pdf",
			Name:          "arch-topology-2026.pdf",
			ApparentBytes: 655670,
			DiskBytes:     659456,
		},
		{
			RelPath:       "Attachments/K8s/cluster-rollout-plan.png",
			Name:          "cluster-rollout-plan.png",
			ApparentBytes: 425612,
			DiskBytes:     425984,
		},
		{
			RelPath:       "Attachments/Security/pentest-summary.pdf",
			Name:          "pentest-summary.pdf",
			ApparentBytes: 198644,
			DiskBytes:     200704,
		},
		{
			RelPath:       "Attachments/Latency/p99-flamegraph.pdf",
			Name:          "p99-flamegraph.pdf",
			ApparentBytes: 79386,
			DiskBytes:     81920,
		},
		{
			RelPath:       "Engineering/Attachments/ServiceMesh/mesh-diagram.png",
			Name:          "mesh-diagram.png",
			ApparentBytes: 1845000,
			DiskBytes:     1847296,
		},
		// Orphans in Attachments/
		{
			RelPath:       "Attachments/unreferenced-screenshot-1.png",
			Name:          "unreferenced-screenshot-1.png",
			ApparentBytes: 1420000,
			DiskBytes:     1421312,
		},
		{
			RelPath:       "Attachments/old-backup-archive.zip",
			Name:          "old-backup-archive.zip",
			ApparentBytes: 1830580,
			DiskBytes:     1830912,
		},
		// System files
		{
			RelPath:       ".obsidian/plugins/obdu/main.js",
			Name:          "main.js",
			ApparentBytes: 95200,
			DiskBytes:     98304,
		},
	}

	return graph.BuildVaultGraph("KnowledgeBase", "/Users/alex/KnowledgeBase", true, entries, nil)
}

func TestUISimulationScreenAndNavigation(t *testing.T) {
	vg := buildDemoVaultGraph()
	app := NewApp(Config{
		VaultRoot:      "/Users/alex/KnowledgeBase",
		VaultName:      "KnowledgeBase",
		InitialRelPath: "Projects",
		IsVault:        true,
		SizeMode:       model.SizeLogical,
		BlockMode:      model.BlockApparent,
		ShowHidden:     true,
	}, vg)

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	defer sim.Fini()

	sim.SetSize(112, 16)
	app.screen = sim
	app.selectedIndex = 2 // Highlight second note so row 1's red '!' flag is visible
	app.render()

	if dumpPath := os.Getenv("OBDU_DUMP_SCREEN"); dumpPath != "" {
		if err := dumpSimScreen(sim, dumpPath); err != nil {
			t.Fatalf("failed to dump screen: %v", err)
		}
	}

	// Verify drilling into the selected note and popping back with Backspace
	app.handleKeyEvent(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	app.render()
	if app.currentMode != model.ModeNote {
		t.Fatalf("expected ModeNote after pressing Enter on note, got %s", app.currentMode)
	}

	app.handleKeyEvent(tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone))
	app.render()
	if app.currentMode != model.ModeDir || app.currentPath != "Projects" {
		t.Fatalf("expected back in ModeDir 'Projects', got mode=%s path=%q", app.currentMode, app.currentPath)
	}
}
