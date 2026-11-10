package actions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarkusLitz/obdu/internal/graph"
	"github.com/MarkusLitz/obdu/internal/model"
	"github.com/MarkusLitz/obdu/internal/scanner"
)

func TestEnsureInsideRootSecurityBoundaries(t *testing.T) {
	baseDir := t.TempDir()
	vaultRoot := filepath.Join(baseDir, "vault")
	evilSibling := filepath.Join(baseDir, "vault-evil")

	if err := os.MkdirAll(filepath.Join(vaultRoot, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evilSibling, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(evilSibling, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink(outsideFile, filepath.Join(vaultRoot, "symlink-note.md"))

	rejected := []string{
		"",
		".",
		"..",
		"/",
		".obsidian",
		"../outside.md",
		"Projects/../../outside.md",
		"/etc/passwd",
		"Note\x00Evil.md",
		filepath.Join(evilSibling, "file.md"),
		"symlink-note.md",
	}

	for _, candidate := range rejected {
		if got, err := EnsureInsideRoot(vaultRoot, candidate); err == nil {
			t.Fatalf("expected EnsureInsideRoot(%q) to fail, but got %q", candidate, got)
		}
	}

	validRel := "Projects/Architecture.md"
	got, err := EnsureInsideRoot(vaultRoot, validRel)
	if err != nil {
		t.Fatalf("expected valid path %q to succeed, got error: %v", validRel, err)
	}
	if !strings.HasSuffix(got, filepath.FromSlash("Projects/Architecture.md")) {
		t.Fatalf("unexpected resolved path: %q", got)
	}
}

func TestSmartDeleteCascadeAndTrashCollision(t *testing.T) {
	vaultRoot := t.TempDir()
	mustWrite := func(rel, content string) {
		full := filepath.Join(vaultRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite(".obsidian/app.json", `{}`)
	mustWrite("Projects/TargetNote.md", "Uses ![[exclusive.png]] and ![[shared.png]]")
	mustWrite("Projects/OtherNote.md", "Links to [[TargetNote]] and embeds ![[shared.png]]")
	mustWrite("Attachments/TargetNote/exclusive.png", "EXCLUSIVE_BYTES")
	mustWrite("Attachments/shared.png", "SHARED_BYTES")
	// Pre-populate .trash/Projects/TargetNote.md to test timestamp collision handling!
	mustWrite(".trash/Projects/TargetNote.md", "OLD_TRASH_VERSION")

	entries, err := scanner.ScanRoot(vaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	vg := graph.BuildVaultGraph("KnowledgeBase", vaultRoot, true, entries, nil)

	targetNode := vg.NodesByPath["Projects/TargetNote.md"]
	if targetNode == nil {
		t.Fatalf("missing Projects/TargetNote.md")
	}

	plan := BuildDeletePlan(targetNode)
	if len(plan.ExclusiveAttachments) != 1 || plan.ExclusiveAttachments[0].RelPath != "Attachments/TargetNote/exclusive.png" {
		t.Fatalf("expected 1 exclusive attachment, got %#v", plan.ExclusiveAttachments)
	}
	if len(plan.SharedAttachments) != 1 || plan.SharedAttachments[0].RelPath != "Attachments/shared.png" {
		t.Fatalf("expected 1 shared attachment, got %#v", plan.SharedAttachments)
	}
	if len(plan.IncomingBacklinks) != 1 || plan.IncomingBacklinks[0].RelPath != "Projects/OtherNote.md" {
		t.Fatalf("expected 1 incoming backlink from OtherNote.md, got %#v", plan.IncomingBacklinks)
	}

	err = ExecuteDelete(vaultRoot, true, plan, model.DeleteOptions{
		Permanent:                   false,
		IncludeExclusiveAttachments: true,
	})
	if err != nil {
		t.Fatalf("ExecuteDelete failed: %v", err)
	}

	// 1. TargetNote.md and exclusive.png moved out of active folders:
	if _, err := os.Stat(filepath.Join(vaultRoot, "Projects/TargetNote.md")); !os.IsNotExist(err) {
		t.Fatalf("expected Projects/TargetNote.md to be moved to .trash")
	}
	if _, err := os.Stat(filepath.Join(vaultRoot, "Attachments/TargetNote/exclusive.png")); !os.IsNotExist(err) {
		t.Fatalf("expected exclusive.png to be moved to .trash")
	}

	// 2. Emptied per-note subfolder Attachments/TargetNote cleaned up, while root Attachments/ remains!
	if _, err := os.Stat(filepath.Join(vaultRoot, "Attachments/TargetNote")); !os.IsNotExist(err) {
		t.Fatalf("expected empty per-note subfolder Attachments/TargetNote to be removed")
	}
	if st, err := os.Stat(filepath.Join(vaultRoot, "Attachments")); err != nil || !st.IsDir() {
		t.Fatalf("expected root Attachments/ directory to be preserved")
	}

	// 3. Shared attachment untouched:
	if _, err := os.Stat(filepath.Join(vaultRoot, "Attachments/shared.png")); err != nil {
		t.Fatalf("expected shared.png to remain untouched: %v", err)
	}

	// 4. Both original .trash/Projects/TargetNote.md and timestamped collision file exist in .trash/Projects:
	trashProjectsEntries, err := os.ReadDir(filepath.Join(vaultRoot, ".trash/Projects"))
	if err != nil {
		t.Fatalf("failed reading .trash/Projects: %v", err)
	}
	if len(trashProjectsEntries) != 2 {
		t.Fatalf("expected 2 files in .trash/Projects after collision, got %d", len(trashProjectsEntries))
	}

	// 5. Exclusive attachment exists at .trash/Attachments/TargetNote/exclusive.png:
	if _, err := os.Stat(filepath.Join(vaultRoot, ".trash/Attachments/TargetNote/exclusive.png")); err != nil {
		t.Fatalf("expected exclusive.png in .trash/Attachments/TargetNote/exclusive.png: %v", err)
	}
}
