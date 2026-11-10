package graph

import (
	"testing"

	"github.com/MarkusLitz/obdu/internal/model"
)

func TestFourTierResolutionAndHiddenGraphExclusion(t *testing.T) {
	entries := []model.ScannedEntry{
		{
			RelPath:       "Engineering/ServiceMesh.md",
			Name:          "ServiceMesh.md",
			ApparentBytes: 500,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "Overview: ![[mesh-diagram.png|492]] and [[Projects/Roadmap]]",
		},
		{
			RelPath:       "Engineering/Attachments/ServiceMesh/mesh-diagram.png",
			Name:          "mesh-diagram.png",
			ApparentBytes: 250000,
			DiskBytes:     253952,
		},
		{
			RelPath:       "Projects/Roadmap.md",
			Name:          "Roadmap.md",
			ApparentBytes: 300,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "See [Legacy](./_resources/Roadmap.resources/spec.pdf) and [[MissingNote]]",
		},
		{
			RelPath:       "Projects/_resources/Roadmap.resources/spec.pdf",
			Name:          "spec.pdf",
			ApparentBytes: 80000,
			DiskBytes:     81920,
		},
		{
			RelPath:       "Attachments/orphan.png",
			Name:          "orphan.png",
			ApparentBytes: 42000,
			DiskBytes:     45056,
		},
		{
			RelPath:       "Attachments/bookmarked.pdf",
			Name:          "bookmarked.pdf",
			ApparentBytes: 12000,
			DiskBytes:     12288,
		},
		{
			// Deleted note in .trash referencing orphan.png MUST NOT rescue orphan.png!
			RelPath:       ".trash/OldNote.md",
			Name:          "OldNote.md",
			ApparentBytes: 200,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "![[orphan.png]]",
		},
		{
			// Plugin README in .obsidian referencing orphan.png MUST NOT rescue orphan.png!
			RelPath:       ".obsidian/plugins/foo/README.md",
			Name:          "README.md",
			ApparentBytes: 900,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "![[orphan.png]]",
		},
	}

	bookmarks := map[string]bool{
		"Attachments/bookmarked.pdf": true,
	}

	vg := BuildVaultGraph("KnowledgeBase", "/tmp/KnowledgeBase", true, entries, bookmarks)

	mesh := vg.NodesByPath["Engineering/ServiceMesh.md"]
	if mesh == nil {
		t.Fatalf("missing Engineering/ServiceMesh.md")
	}
	if len(mesh.OutgoingAttachments) != 1 || mesh.OutgoingAttachments[0].RelPath != "Engineering/Attachments/ServiceMesh/mesh-diagram.png" {
		t.Fatalf("expected basename attachment resolution, got %#v", mesh.OutgoingAttachments)
	}
	// Note with 0 incoming backlinks must NEVER get 'O' flag in tree!
	if flag := GetNodeFlag(mesh, true); flag == 'O' {
		t.Fatalf("notes with 0 backlinks must never have 'O' flag, got %q", flag)
	}

	roadmap := vg.NodesByPath["Projects/Roadmap.md"]
	if len(roadmap.OutgoingAttachments) != 1 || roadmap.OutgoingAttachments[0].RelPath != "Projects/_resources/Roadmap.resources/spec.pdf" {
		t.Fatalf("expected ./_resources resolution, got %#v", roadmap.OutgoingAttachments)
	}
	if GetNodeFlag(roadmap, true) != '!' {
		t.Fatalf("expected broken link flag '!' on Roadmap.md, got %q", GetNodeFlag(roadmap, true))
	}

	orphan := vg.NodesByPath["Attachments/orphan.png"]
	if len(orphan.IncomingNoteOwners) != 0 {
		t.Fatalf("expected orphan.png to have 0 active owners despite .trash/.obsidian links, got %d", len(orphan.IncomingNoteOwners))
	}
	if GetNodeFlag(orphan, true) != 'O' {
		t.Fatalf("expected 'O' flag on unreferenced attachment, got %q", GetNodeFlag(orphan, true))
	}

	bm := vg.NodesByPath["Attachments/bookmarked.pdf"]
	if GetNodeFlag(bm, true) != ' ' {
		t.Fatalf("expected bookmarked attachment to be protected from 'O' flag, got %q", GetNodeFlag(bm, true))
	}
}

func TestExactByteConservationAndSharedRemainderDistribution(t *testing.T) {
	entries := []model.ScannedEntry{
		{
			RelPath:       "Projects/NoteA.md",
			Name:          "NoteA.md",
			ApparentBytes: 200,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "Shared: ![[shared_odd.png]] and exclusive: ![[exclusive_a.png]]",
		},
		{
			RelPath:       "Projects/NoteB.md",
			Name:          "NoteB.md",
			ApparentBytes: 300,
			DiskBytes:     4096,
			HasContent:    true,
			Content:       "Also uses ![[shared_odd.png]]",
		},
		{
			RelPath:       "Attachments/shared_odd.png",
			Name:          "shared_odd.png",
			ApparentBytes: 1001, // Odd byte count shared across 2 notes -> 501 + 500
			DiskBytes:     4097, // Intentionally odd for test
		},
		{
			RelPath:       "Attachments/exclusive_a.png",
			Name:          "exclusive_a.png",
			ApparentBytes: 5000,
			DiskBytes:     8192,
		},
		{
			RelPath:       "Attachments/orphan_dead.png",
			Name:          "orphan_dead.png",
			ApparentBytes: 7777,
			DiskBytes:     8192,
		},
		{
			RelPath:       ".obsidian/plugins/obdu/main.js",
			Name:          "main.js",
			ApparentBytes: 93127,
			DiskBytes:     94208,
		},
		{
			RelPath:       ".trash/deleted.bin",
			Name:          "deleted.bin",
			ApparentBytes: 12345,
			DiskBytes:     16384,
		},
	}

	vg := BuildVaultGraph("KnowledgeBase", "/tmp/KnowledgeBase", true, entries, nil)

	if vg.TotalPhysicalBytes != vg.TotalLogicalBytes {
		t.Fatalf("root conservation failed on build: physical=%d != logical=%d", vg.TotalPhysicalBytes, vg.TotalLogicalBytes)
	}

	blockModes := []model.BlockMode{model.BlockApparent, model.BlockDisk}
	hiddenModes := []bool{true, false}

	for _, bm := range blockModes {
		for _, showHidden := range hiddenModes {
			RecomputeDisplaySizes(vg.Root, vg.AllFiles, model.SizePhysical, bm, showHidden)
			physTotal := vg.Root.DisplayBytes

			RecomputeDisplaySizes(vg.Root, vg.AllFiles, model.SizeLogical, bm, showHidden)
			logTotal := vg.Root.DisplayBytes

			if physTotal != logTotal {
				t.Fatalf("conservation law violated for blockMode=%s showHidden=%v: physical=%d logical=%d",
					bm, showHidden, physTotal, logTotal)
			}
		}
	}

	// Check odd-byte split (1001 B -> 501 B to NoteA, 500 B to NoteB) in Apparent + Logical mode:
	RecomputeDisplaySizes(vg.Root, vg.AllFiles, model.SizeLogical, model.BlockApparent, true)
	noteA := vg.NodesByPath["Projects/NoteA.md"]
	noteB := vg.NodesByPath["Projects/NoteB.md"]
	sharedAtt := vg.NodesByPath["Attachments/shared_odd.png"]
	attDir := vg.NodesByPath["Attachments"]

	// NoteA = 200 (self) + 5000 (exclusive_a) + 501 (share of shared_odd) = 5701
	if noteA.DisplayBytes != 5701 {
		t.Fatalf("expected NoteA logical size 5701, got %d", noteA.DisplayBytes)
	}
	// NoteB = 300 (self) + 500 (share of shared_odd) = 800
	if noteB.DisplayBytes != 800 {
		t.Fatalf("expected NoteB logical size 800, got %d", noteB.DisplayBytes)
	}
	// Attributed attachment in Attachments/ has 0 B in Logical mode
	if sharedAtt.DisplayBytes != 0 {
		t.Fatalf("expected shared_odd.png logical size 0 in Attachments/, got %d", sharedAtt.DisplayBytes)
	}
	// Attachments/ directory in Logical mode retains ONLY the orphan (7777 B)!
	if attDir.DisplayBytes != 7777 {
		t.Fatalf("expected Attachments/ logical size to equal orphan weight 7777, got %d", attDir.DisplayBytes)
	}
}
