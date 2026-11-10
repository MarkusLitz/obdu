package actions

import (
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func startBackgroundProcess(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

// OpenInObsidian opens the given vault-relative file inside the running Obsidian application
// via the obsidian://open URI scheme without invoking a shell.
func OpenInObsidian(vaultName, vaultRoot, relPath string) error {
	if _, err := EnsureInsideRoot(vaultRoot, relPath); err != nil {
		return err
	}
	uri := "obsidian://open?vault=" + url.QueryEscape(vaultName) + "&file=" + url.QueryEscape(relPath)
	if runtime.GOOS == "darwin" {
		return startBackgroundProcess("/usr/bin/open", uri)
	}
	return startBackgroundProcess("xdg-open", uri)
}

// OpenExternalURL validates that rawURL uses http:// or https:// and opens it in the system browser.
func OpenExternalURL(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" || strings.ContainsAny(trimmed, "\x00\r\n") {
		return errors.New("ungültige URL")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return err
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return errors.New("nur http:// und https:// URLs sind erlaubt")
	}
	if runtime.GOOS == "darwin" {
		return startBackgroundProcess("/usr/bin/open", u.String())
	}
	return startBackgroundProcess("xdg-open", u.String())
}

// RevealInFileManager highlights the selected file in macOS Finder (`open -R -- <path>`)
// or opens its containing folder in the Linux file manager (`xdg-open`).
func RevealInFileManager(vaultRoot, candidatePath string) error {
	safeAbs, err := EnsureInsideRoot(vaultRoot, candidatePath)
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		return startBackgroundProcess("/usr/bin/open", "-R", "--", safeAbs)
	}
	targetDir := safeAbs
	if fi, err := os.Lstat(safeAbs); err == nil && !fi.IsDir() {
		targetDir = filepath.Dir(safeAbs)
	}
	return startBackgroundProcess("xdg-open", targetDir)
}

// PreviewExternal launches macOS QuickLook (`qlmanage -p`) or the system default viewer on Linux.
func PreviewExternal(vaultRoot, candidatePath string) error {
	safeAbs, err := EnsureInsideRoot(vaultRoot, candidatePath)
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		return startBackgroundProcess("/usr/bin/qlmanage", "-p", safeAbs)
	}
	return startBackgroundProcess("xdg-open", safeAbs)
}
