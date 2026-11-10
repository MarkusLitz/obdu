package parser

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/MarkusLitz/obdu/internal/model"
)

var ignoredURIPrefixes = []string{
	"mailto:",
	"tel:",
	"obsidian:",
	"evernote:",
	"javascript:",
	"data:",
	"about:",
	"file:",
}

var (
	ansiCSIRegex       = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	ansiOSCRegex       = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	driveFilePathRegex = regexp.MustCompile(`(?i)drive\.google\.com/file/d/([a-zA-Z0-9_-]+)`)
	driveIDParamRegex  = regexp.MustCompile(`(?i)drive\.google\.com/(?:open|thumbnail|uc)\?(?:[^#]*&)?id=([a-zA-Z0-9_-]+)`)
	yamlRootKeyRegex   = regexp.MustCompile(`^[A-Za-z0-9_-]+:`)
	yamlTaskItemRegex  = regexp.MustCompile(`^[ \t]*-[ \t]+\S+`)
	mdTitleSuffixRegex = regexp.MustCompile(`^(\S+)\s+(?:"[^"]*"|'[^']*')$`)
	wikiLinkRegex      = regexp.MustCompile(`(!?)\[\[([^\[\]\r\n]+)\]\]`)
	mdAngleLinkRegex   = regexp.MustCompile(`(!?)\[([^\[\]\r\n]*)\]\(<([^>\r\n]+)>\)`)
	mdParenLinkRegex   = regexp.MustCompile(`(!?)\[([^\[\]\r\n]*)\]\(([^()\r\n]+(?:\([^()\r\n]*\)[^()\r\n]*)*)\)`)
	htmlMediaTagRegex  = regexp.MustCompile(`(?i)<(?:img|iframe|video|audio|source)\b[^>]*?\bsrc=["']([^"']+)["'][^>]*>`)
	htmlAltAttrRegex   = regexp.MustCompile(`(?i)\balt=["']([^"']+)["']`)
)

// ParseResult contains all extracted references and empty-state diagnostics for a note.
type ParseResult struct {
	LocalTargets   []string
	ExternalRefs   []model.ExternalRef
	HasActiveTasks bool
	EmptyReason    string
}

// SanitizeDisplayString strips ANSI/OSC terminal escape sequences and control characters
// before rendering filenames or link targets in the TUI or CLI output.
func SanitizeDisplayString(input string) string {
	if input == "" {
		return ""
	}
	cleaned := ansiOSCRegex.ReplaceAllString(input, "")
	cleaned = ansiCSIRegex.ReplaceAllString(cleaned, "")
	var b strings.Builder
	b.Grow(len(cleaned))
	for _, r := range cleaned {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || unicode.IsControl(r) {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// ExtractGoogleDriveID extracts a Google Drive file ID from a URL if it matches known Drive patterns.
func ExtractGoogleDriveID(rawURL string) string {
	if m := driveFilePathRegex.FindStringSubmatch(rawURL); len(m) == 2 {
		return m[1]
	}
	if m := driveIDParamRegex.FindStringSubmatch(rawURL); len(m) == 2 {
		return m[1]
	}
	return ""
}

func cleanExternalLabel(rawLabel, fallbackURL string) string {
	trimmed := strings.TrimSpace(rawLabel)
	trimmed = strings.Trim(trimmed, "*")
	trimmed = strings.TrimSpace(trimmed)
	trimmed = strings.TrimPrefix(trimmed, "📁")
	cleaned := SanitizeDisplayString(trimmed)
	lower := strings.ToLower(cleaned)
	if cleaned != "" && !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return cleaned
	}
	if driveID := ExtractGoogleDriveID(fallbackURL); driveID != "" {
		shortID := driveID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		return "Google Drive (" + shortID + "…)"
	}
	if u, err := url.Parse(fallbackURL); err == nil && u.Host != "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) > 0 {
			last := parts[len(parts)-1]
			if unescaped, err := url.PathUnescape(last); err == nil && unescaped != "" {
				last = unescaped
			}
			if last != "" && last != "image.jpg" && last != "preview" && last != "view" {
				return SanitizeDisplayString(last)
			}
		}
		p := u.Path
		if len(p) > 32 {
			p = p[:32]
		}
		return SanitizeDisplayString(u.Host + p)
	}
	if len(fallbackURL) > 48 {
		fallbackURL = fallbackURL[:48]
	}
	return SanitizeDisplayString(fallbackURL)
}

// SplitFrontmatter extracts YAML frontmatter if content starts with "---\n" or "---\r\n".
func SplitFrontmatter(content string) (frontmatter string, hasFrontmatter bool, body string) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return "", false, content
	}
	firstNL := strings.IndexByte(content, '\n')
	if firstNL < 0 {
		return "", false, content
	}
	rest := content[firstNL+1:]
	offset := 0
	for offset <= len(rest) {
		lineEnd := strings.IndexByte(rest[offset:], '\n')
		var line string
		var nextOffset int
		if lineEnd < 0 {
			line = rest[offset:]
			nextOffset = len(rest) + 1
		} else {
			line = rest[offset : offset+lineEnd]
			nextOffset = offset + lineEnd + 1
		}
		trimmedLine := strings.TrimRight(line, " \t\r")
		if trimmedLine == "---" || trimmedLine == "..." {
			fm := rest[:offset]
			var rem string
			if nextOffset <= len(rest) {
				rem = rest[nextOffset:]
			}
			return fm, true, rem
		}
		if lineEnd < 0 {
			break
		}
		offset = nextOffset
	}
	return "", false, content
}

// HasActiveFrontmatterTasks checks whether YAML frontmatter has non-empty root-level `tasks:` items
// (supporting TaskPulse frontmatter-only task notes).
func HasActiveFrontmatterTasks(frontmatter string) bool {
	if frontmatter == "" {
		return false
	}
	lines := strings.Split(strings.ReplaceAll(frontmatter, "\r\n", "\n"), "\n")
	inTasksBlock := false
	for _, line := range lines {
		if !inTasksBlock {
			if strings.HasPrefix(line, "tasks:") {
				inlineVal := strings.TrimSpace(strings.TrimPrefix(line, "tasks:"))
				if inlineVal == "[]" || inlineVal == "null" || inlineVal == "~" {
					return false
				}
				if strings.HasPrefix(inlineVal, "[") && len(inlineVal) > 2 {
					return true
				}
				inTasksBlock = true
			}
			continue
		}
		if yamlRootKeyRegex.MatchString(line) {
			inTasksBlock = false
			continue
		}
		if yamlTaskItemRegex.MatchString(line) {
			return true
		}
	}
	return false
}

// maskInlineCodeLine replaces single-line backtick code spans (`...`, ``...``) with spaces.
func maskInlineCodeLine(line string) string {
	if !strings.ContainsRune(line, '`') {
		return line
	}
	buf := []byte(line)
	n := len(buf)
	i := 0
	for i < n {
		if buf[i] != '`' {
			i++
			continue
		}
		runStart := i
		for i < n && buf[i] == '`' {
			i++
		}
		runLen := i - runStart
		// Search for matching closing backtick run of exact length runLen
		j := i
		foundClose := -1
		for j < n {
			if buf[j] != '`' {
				j++
				continue
			}
			closeStart := j
			for j < n && buf[j] == '`' {
				j++
			}
			if j-closeStart == runLen {
				foundClose = j
				break
			}
		}
		if foundClose >= 0 {
			for k := runStart; k < foundClose; k++ {
				buf[k] = ' '
			}
			i = foundClose
		}
	}
	return string(buf)
}

// MaskCodeSegments masks fenced code blocks (``` or ~~~) and inline backtick code spans.
func MaskCodeSegments(markdown string) string {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	inFence := false
	var fenceChar byte
	fenceLen := 0

	for idx, line := range lines {
		trimmedStart := line
		spaces := 0
		for spaces < len(trimmedStart) && spaces < 3 && (trimmedStart[spaces] == ' ' || trimmedStart[spaces] == '\t') {
			spaces++
		}
		trimmedStart = trimmedStart[spaces:]

		if len(trimmedStart) >= 3 && (trimmedStart[0] == '`' || trimmedStart[0] == '~') {
			ch := trimmedStart[0]
			cnt := 0
			for cnt < len(trimmedStart) && trimmedStart[cnt] == ch {
				cnt++
			}
			if cnt >= 3 {
				if !inFence {
					inFence = true
					fenceChar = ch
					fenceLen = cnt
					lines[idx] = ""
					continue
				} else if ch == fenceChar && cnt >= fenceLen && strings.TrimSpace(trimmedStart[cnt:]) == "" {
					inFence = false
					fenceChar = 0
					fenceLen = 0
					lines[idx] = ""
					continue
				}
			}
		}

		if inFence {
			lines[idx] = ""
			continue
		}
		lines[idx] = maskInlineCodeLine(line)
	}
	return strings.Join(lines, "\n")
}

// NormalizeLocalTargetCandidate normalizes a raw wikilink or markdown link target into a vault-local target string.
// Returns "" if the link should be ignored (self-heading, external URL, mailto, etc.).
func NormalizeLocalTargetCandidate(raw string) string {
	target := strings.TrimSpace(raw)
	if target == "" {
		return ""
	}
	if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
		target = strings.TrimSpace(target[1 : len(target)-1])
	}
	if m := mdTitleSuffixRegex.FindStringSubmatch(target); len(m) == 2 {
		target = m[1]
	}
	lower := strings.ToLower(target)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "//") {
		return ""
	}
	for _, prefix := range ignoredURIPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return ""
		}
	}
	if hashIdx := strings.IndexByte(target, '#'); hashIdx == 0 {
		return ""
	} else if hashIdx > 0 {
		target = target[:hashIdx]
	}
	if qIdx := strings.IndexByte(target, '?'); qIdx >= 0 {
		target = target[:qIdx]
	}
	if unescaped, err := url.PathUnescape(target); err == nil {
		target = unescaped
	}
	target = strings.TrimSpace(strings.ReplaceAll(target, "\\", "/"))
	return target
}

func isHTTPURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

type canvasDoc struct {
	Nodes []canvasNode `json:"nodes"`
}

type canvasNode struct {
	Type string `json:"type"`
	File string `json:"file"`
	Text string `json:"text"`
	URL  string `json:"url"`
}

// ParseNoteContent parses a .md, .base, or .canvas file's content according to obdu's link & empty-state rules.
func ParseNoteContent(content string, ext string) ParseResult {
	lowerExt := strings.ToLower(strings.TrimPrefix(ext, "."))

	if len(content) == 0 {
		return ParseResult{
			EmptyReason: "0 Bytes",
		}
	}
	if strings.TrimSpace(content) == "" {
		return ParseResult{
			EmptyReason: "Nur Leerzeichen",
		}
	}

	var localTargets []string
	localSeen := make(map[string]bool)
	addLocal := func(candidate string) {
		loc := NormalizeLocalTargetCandidate(candidate)
		if loc != "" && !localSeen[loc] {
			localSeen[loc] = true
			localTargets = append(localTargets, loc)
		}
	}

	var driveOrder []string
	driveByID := make(map[string]model.ExternalRef)
	var webOrder []string
	webByURL := make(map[string]model.ExternalRef)

	addExternal := func(rawURL, rawLabel string, isMediaEmbed bool) {
		trimmedURL := strings.TrimSpace(rawURL)
		if driveID := ExtractGoogleDriveID(trimmedURL); driveID != "" {
			candidateLabel := cleanExternalLabel(rawLabel, trimmedURL)
			existing, exists := driveByID[driveID]
			if !exists {
				driveOrder = append(driveOrder, driveID)
				driveByID[driveID] = model.ExternalRef{
					URL:      trimmedURL,
					Label:    candidateLabel,
					Provider: "gdrive",
				}
			} else if strings.HasPrefix(existing.Label, "Google Drive (") && !strings.HasPrefix(candidateLabel, "Google Drive (") {
				driveByID[driveID] = model.ExternalRef{
					URL:      trimmedURL,
					Label:    candidateLabel,
					Provider: "gdrive",
				}
			}
			return
		}
		if isMediaEmbed && isHTTPURL(trimmedURL) {
			if _, exists := webByURL[trimmedURL]; !exists {
				webOrder = append(webOrder, trimmedURL)
				webByURL[trimmedURL] = model.ExternalRef{
					URL:      trimmedURL,
					Label:    cleanExternalLabel(rawLabel, trimmedURL),
					Provider: "web",
				}
			}
		}
	}

	buildExternalSlice := func() []model.ExternalRef {
		out := make([]model.ExternalRef, 0, len(driveOrder)+len(webOrder))
		for _, id := range driveOrder {
			out = append(out, driveByID[id])
		}
		for _, u := range webOrder {
			out = append(out, webByURL[u])
		}
		return out
	}

	if lowerExt == "canvas" {
		var doc canvasDoc
		if err := json.Unmarshal([]byte(content), &doc); err == nil {
			if len(doc.Nodes) == 0 {
				return ParseResult{
					EmptyReason: "Nur Frontmatter",
				}
			}
			for _, n := range doc.Nodes {
				switch n.Type {
				case "file":
					if n.File != "" {
						addLocal(n.File)
					}
				case "text":
					if n.Text != "" {
						inner := ParseNoteContent(n.Text, "md")
						for _, t := range inner.LocalTargets {
							addLocal(t)
						}
						for _, e := range inner.ExternalRefs {
							addExternal(e.URL, e.Label, true)
						}
					}
				case "link":
					if n.URL != "" {
						addExternal(n.URL, n.URL, true)
					}
				}
			}
		}
		return ParseResult{
			LocalTargets: localTargets,
			ExternalRefs: buildExternalSlice(),
		}
	}

	frontmatter, hasFrontmatter, body := SplitFrontmatter(content)
	hasActiveTasks := HasActiveFrontmatterTasks(frontmatter)
	emptyReason := ""
	if hasFrontmatter && strings.TrimSpace(body) == "" && !hasActiveTasks {
		emptyReason = "Nur Frontmatter"
	}

	if hasFrontmatter && frontmatter != "" {
		for _, m := range wikiLinkRegex.FindAllStringSubmatch(frontmatter, -1) {
			parts := strings.SplitN(m[2], "|", 2)
			addLocal(parts[0])
		}
	}

	masked := MaskCodeSegments(body)

	// 1. Wikilinks & embeds: ![[target|alias]] or [[target|alias]]
	for _, wm := range wikiLinkRegex.FindAllStringSubmatch(masked, -1) {
		isEmbed := wm[1] == "!"
		parts := strings.SplitN(wm[2], "|", 2)
		rawTarget := strings.TrimSpace(parts[0])
		alias := rawTarget
		if len(parts) > 1 {
			alias = parts[1]
		}
		if isHTTPURL(rawTarget) {
			addExternal(rawTarget, alias, isEmbed)
			continue
		}
		addLocal(rawTarget)
	}

	// 2. Angle-bracket Markdown links: [label](<url>)
	for _, mam := range mdAngleLinkRegex.FindAllStringSubmatch(masked, -1) {
		isEmbed := mam[1] == "!"
		label := strings.SplitN(mam[2], "|", 2)[0]
		rawURL := strings.TrimSpace(mam[3])
		if isHTTPURL(rawURL) {
			addExternal(rawURL, label, isEmbed)
		} else {
			addLocal(rawURL)
		}
	}

	// 3. Standard Markdown links: [label](url) with balanced single-level parentheses
	for _, mm := range mdParenLinkRegex.FindAllStringSubmatch(masked, -1) {
		isEmbed := mm[1] == "!"
		label := strings.SplitN(mm[2], "|", 2)[0]
		rawURL := strings.TrimSpace(mm[3])
		if strings.HasPrefix(rawURL, "<") && strings.HasSuffix(rawURL, ">") {
			continue
		}
		if isHTTPURL(rawURL) {
			addExternal(rawURL, label, isEmbed)
		} else {
			addLocal(rawURL)
		}
	}

	// 4. HTML media tags: <img|iframe|video|audio|source ... src="...">
	for _, hm := range htmlMediaTagRegex.FindAllStringSubmatch(masked, -1) {
		src := strings.TrimSpace(hm[1])
		label := ""
		if altMatch := htmlAltAttrRegex.FindStringSubmatch(hm[0]); len(altMatch) == 2 {
			label = altMatch[1]
		}
		if isHTTPURL(src) {
			addExternal(src, label, true)
		} else {
			addLocal(src)
		}
	}

	return ParseResult{
		LocalTargets:   localTargets,
		ExternalRefs:   buildExternalSlice(),
		HasActiveTasks: hasActiveTasks,
		EmptyReason:    emptyReason,
	}
}
