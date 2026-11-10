package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindVaultRootAndSymlinkRejection(t *testing.T) {
	tmp := t.TempDir()
	vaultDir := filepath.Join(tmp, "KnowledgeBase")
	subDir := filepath.Join(vaultDir, "Engineering", "Attachments")
	outsideDir := filepath.Join(tmp, "outside-secret")

	if err := os.MkdirAll(filepath.Join(vaultDir, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "secret.md"), []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "Engineering", "Overview.md"), []byte("# Overview"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside vault pointing outside -> ScanRoot MUST ignore it!
	_ = os.Symlink(outsideDir, filepath.Join(vaultDir, "evil-link"))

	root, initRel, isVault, err := FindVaultRoot(subDir)
	if err != nil {
		t.Fatalf("FindVaultRoot failed: %v", err)
	}
	if !isVault {
		t.Fatalf("expected isVault=true")
	}
	if root != vaultDir {
		t.Fatalf("expected vaultRoot=%q, got %q", vaultDir, root)
	}
	if initRel != "Engineering/Attachments" {
		t.Fatalf("expected initRel='Engineering/Attachments', got %q", initRel)
	}

	entries, err := ScanRoot(vaultDir)
	if err != nil {
		t.Fatalf("ScanRoot failed: %v", err)
	}
	for _, e := range entries {
		if e.Name == "evil-link" || e.Name == "secret.md" {
			t.Fatalf("ScanRoot traversed or included a symlink: %#v", e)
		}
	}
}
