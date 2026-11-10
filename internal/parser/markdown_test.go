package parser

import (
	"strings"
	"testing"
)

func TestCodeMaskingPreventsFalsePositiveLinks(t *testing.T) {
	content := `---
title: My Obsidian Setup
---
Here is how PDF embeds work:

` + "```css\n/* Syntax: ![[file.pdf|small]] */\n.pdf-embed { width: 100%; }\n```" + `

~~~markdown
![[fenced-tilde.png]]
~~~

And inline code like ` + "`![[inline-ignored.png]]`" + ` or ` + "``[[double-backtick.md]]``" + ` must also be ignored.
Real embed: ![[active-diagram.png|492]]
`
	res := ParseNoteContent(content, "md")
	if len(res.LocalTargets) != 1 || res.LocalTargets[0] != "active-diagram.png" {
		t.Fatalf("expected only [active-diagram.png], got %#v", res.LocalTargets)
	}
}

func TestTaskPulseFrontmatterDetection(t *testing.T) {
	emptyTasksNote := "---\ntitle: Empty Task Note\ntasks: []\ntags:\n  - test\n---\n   \n"
	resEmpty := ParseNoteContent(emptyTasksNote, "md")
	if resEmpty.HasActiveTasks {
		t.Fatalf("expected HasActiveTasks=false for tasks: []")
	}
	if resEmpty.EmptyReason != "Nur Frontmatter" {
		t.Fatalf("expected EmptyReason='Nur Frontmatter', got %q", resEmpty.EmptyReason)
	}

	activeTasksNote := "---\ntitle: Recurring Sprint Tasks\ntasks:\n  - id: 1\n    text: Review pull requests\n---\n"
	resActive := ParseNoteContent(activeTasksNote, "md")
	if !resActive.HasActiveTasks {
		t.Fatalf("expected HasActiveTasks=true for active YAML task list")
	}
	if resActive.EmptyReason != "" {
		t.Fatalf("expected EmptyReason='', got %q", resActive.EmptyReason)
	}
}

func TestDriveEmbedderDeduplicationAndWebLinks(t *testing.T) {
	content := `# Invoices
**[📁 Invoice-2026-0001.pdf](https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz123456/view?usp=drivesdk)**
<iframe src="https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz123456/preview" width="100%" height="480"></iframe>

Normal bookmark: [GitHub](https://github.com/example/repo)
Web image: ![Banner](https://example.com/assets/banner.webp)
`
	res := ParseNoteContent(content, "md")
	if len(res.LocalTargets) != 0 {
		t.Fatalf("expected 0 local targets, got %#v", res.LocalTargets)
	}
	if len(res.ExternalRefs) != 2 {
		t.Fatalf("expected 2 external refs (1 deduplicated gdrive + 1 web image), got %#v", res.ExternalRefs)
	}
	if res.ExternalRefs[0].Provider != "gdrive" || res.ExternalRefs[0].Label != "Invoice-2026-0001.pdf" {
		t.Fatalf("unexpected gdrive ref: %#v", res.ExternalRefs[0])
	}
	if res.ExternalRefs[1].Provider != "web" || res.ExternalRefs[1].Label != "Banner" {
		t.Fatalf("unexpected web ref: %#v", res.ExternalRefs[1])
	}
}

func TestBalancedParensAndPercentEncodingInMarkdownLinks(t *testing.T) {
	content := `See [Roadmap](Product%20Roadmap%20(2026).md) and ![Res](./_resources/Note.resources/img_1.png).`
	res := ParseNoteContent(content, "md")
	if len(res.LocalTargets) != 2 {
		t.Fatalf("expected 2 local targets, got %#v", res.LocalTargets)
	}
	if res.LocalTargets[0] != "Product Roadmap (2026).md" {
		t.Fatalf("unexpected first target: %q", res.LocalTargets[0])
	}
	if res.LocalTargets[1] != "./_resources/Note.resources/img_1.png" {
		t.Fatalf("unexpected second target: %q", res.LocalTargets[1])
	}
}

func TestCanvasExtraction(t *testing.T) {
	emptyCanvas := `{"nodes":[],"edges":[]}`
	if r := ParseNoteContent(emptyCanvas, "canvas"); r.EmptyReason != "Nur Frontmatter" {
		t.Fatalf("expected empty canvas to report 'Nur Frontmatter', got %q", r.EmptyReason)
	}

	canvasJSON := `{
		"nodes": [
			{"id": "1", "type": "file", "file": "Attachments/chart.png"},
			{"id": "2", "type": "text", "text": "Check [[Projects/Architecture]] and ![pic](https://example.com/pic.png)"},
			{"id": "3", "type": "link", "url": "https://drive.google.com/file/d/998877665544332211/view"}
		]
	}`
	res := ParseNoteContent(canvasJSON, "canvas")
	if len(res.LocalTargets) != 2 {
		t.Fatalf("expected 2 local targets from canvas, got %#v", res.LocalTargets)
	}
	if len(res.ExternalRefs) != 2 {
		t.Fatalf("expected 2 external refs from canvas, got %#v", res.ExternalRefs)
	}
}

func TestSanitizeDisplayStringStripsEscapeSequences(t *testing.T) {
	malicious := "\x1b]0;pwned-title\x07\x1b[31mEvil\r\nFile\x00Name.md\x1b[0m"
	got := SanitizeDisplayString(malicious)
	if strings.ContainsAny(got, "\x1b\x07\r\n\x00") {
		t.Fatalf("escape or control characters survived sanitization: %q", got)
	}
	if got != "Evil  File Name.md" {
		t.Fatalf("unexpected sanitized string: %q", got)
	}
}
