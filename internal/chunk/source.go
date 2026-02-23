package chunk

import "strings"

const (
	legacyWindowSize    = 30
	sourceOverlapLines  = 5
	minChars            = 20
	fallbackTargetChars = 1400
	fallbackMaxChars    = 2600
	fallbackMinBlockLen = 80
)

type lineBlock struct {
	start int // 0-based, inclusive
	end   int // 0-based, inclusive
	kind  string
}

func chunkSource(content string) []Chunk {
	lines := strings.Split(content, "\n")
	prefix := buildLinePrefix(lines)

	blocks := detectSourceBlocks(lines, prefix)
	structured := assembleSourceChunks(lines, prefix, blocks)
	if len(structured) == 0 {
		return chunkSourceLegacy(lines)
	}

	// Preserve legacy behavior for pathological over-fragmentation.
	legacy := chunkSourceLegacy(lines)
	if len(legacy) > 0 && len(structured) > len(legacy)*4 {
		return legacy
	}

	return structured
}

func chunkSourceLegacy(lines []string) []Chunk {
	var chunks []Chunk

	for start := 0; start < len(lines); start += legacyWindowSize - sourceOverlapLines {
		end := min(start+legacyWindowSize, len(lines))

		text := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		if len(text) < minChars {
			continue
		}

		chunks = append(chunks, Chunk{
			Text:      text,
			StartLine: start + 1,
			EndLine:   end,
			Kind:      "window",
		})

		if end == len(lines) {
			break
		}
	}

	return chunks
}

func detectSourceBlocks(lines []string, prefix []int) []lineBlock {
	var blocks []lineBlock
	start := -1

	for i := range lines {
		if strings.TrimSpace(lines[i]) == "" {
			if start != -1 {
				blocks = appendSourceSpanBlocks(lines, prefix, blocks, start, i-1)
				start = -1
			}
			continue
		}
		if start == -1 {
			start = i
		}
	}

	if start != -1 {
		blocks = appendSourceSpanBlocks(lines, prefix, blocks, start, len(lines)-1)
	}

	return mergeTinyBlocks(prefix, blocks)
}

func appendSourceSpanBlocks(lines []string, prefix []int, blocks []lineBlock, start, end int) []lineBlock {
	if start > end {
		return blocks
	}

	anchors := splitAnchors(lines, start, end)
	segmentStart := start

	for _, anchor := range anchors {
		if anchor <= segmentStart || anchor > end {
			continue
		}
		blocks = appendSegmentBlocks(lines, prefix, blocks, segmentStart, anchor-1)
		segmentStart = anchor
	}

	return appendSegmentBlocks(lines, prefix, blocks, segmentStart, end)
}

func appendSegmentBlocks(lines []string, prefix []int, blocks []lineBlock, start, end int) []lineBlock {
	if start > end {
		return blocks
	}

	for _, b := range splitSegmentBySize(prefix, start, end) {
		b.kind = classifyBlockKind(lines, b.start, b.end)
		blocks = append(blocks, b)
	}
	return blocks
}

func splitSegmentBySize(prefix []int, start, end int) []lineBlock {
	if start > end {
		return nil
	}
	if spanChars(prefix, start, end) <= fallbackMaxChars {
		return []lineBlock{{start: start, end: end}}
	}

	var blocks []lineBlock
	segStart := start

	for segStart <= end {
		segEnd := segStart
		for segEnd < end && spanChars(prefix, segStart, segEnd) < fallbackTargetChars {
			segEnd++
		}

		if spanChars(prefix, segStart, segEnd) > fallbackMaxChars {
			for segEnd > segStart && spanChars(prefix, segStart, segEnd) > fallbackMaxChars {
				segEnd--
			}
		}
		if segEnd < segStart {
			segEnd = segStart
		}

		blocks = append(blocks, lineBlock{start: segStart, end: segEnd})
		segStart = segEnd + 1
	}

	return blocks
}

func mergeTinyBlocks(prefix []int, blocks []lineBlock) []lineBlock {
	if len(blocks) <= 1 {
		return blocks
	}

	merged := make([]lineBlock, 0, len(blocks))
	for i := 0; i < len(blocks); i++ {
		current := blocks[i]
		if spanChars(prefix, current.start, current.end) >= fallbackMinBlockLen {
			merged = append(merged, current)
			continue
		}

		if i+1 < len(blocks) && blocks[i+1].kind == current.kind {
			current.end = blocks[i+1].end
			i++
			merged = append(merged, current)
			continue
		}

		if len(merged) > 0 && merged[len(merged)-1].kind == current.kind {
			merged[len(merged)-1].end = current.end
		} else {
			merged = append(merged, current)
		}
	}

	return merged
}

func assembleSourceChunks(lines []string, prefix []int, blocks []lineBlock) []Chunk {
	if len(blocks) == 0 {
		return nil
	}

	var chunks []Chunk
	for startIdx := 0; startIdx < len(blocks); {
		startLine := blocks[startIdx].start
		endIdx := startIdx
		endLine := blocks[endIdx].end

		for endIdx+1 < len(blocks) {
			nextEnd := blocks[endIdx+1].end
			if spanChars(prefix, startLine, nextEnd) > fallbackMaxChars &&
				spanChars(prefix, startLine, endLine) >= minChars {
				break
			}
			endIdx++
			endLine = blocks[endIdx].end
			if spanChars(prefix, startLine, endLine) >= fallbackTargetChars {
				break
			}
		}

		chunkStartLine := startLine
		if len(chunks) > 0 {
			chunkStartLine = max(0, startLine-sourceOverlapLines)
			for chunkStartLine < startLine && strings.TrimSpace(lines[chunkStartLine]) == "" {
				chunkStartLine++
			}
		}

		text := strings.TrimSpace(strings.Join(lines[chunkStartLine:endLine+1], "\n"))
		if len(text) >= minChars {
			chunks = append(chunks, Chunk{
				Text:      text,
				StartLine: chunkStartLine + 1,
				EndLine:   endLine + 1,
				Kind:      "window",
			})
		}

		if endIdx == len(blocks)-1 {
			break
		}

		overlapStartLine := max(startLine, endLine-sourceOverlapLines+1)
		nextStartIdx := endIdx + 1
		for nextStartIdx > startIdx+1 && blocks[nextStartIdx-1].end >= overlapStartLine {
			nextStartIdx--
		}
		startIdx = nextStartIdx
	}

	return chunks
}

func splitAnchors(lines []string, start, end int) []int {
	if start >= end {
		return nil
	}

	var anchors []int
	braceDepth := 0

	for i := start; i <= end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}

		if i > start {
			prevTrimmed := strings.TrimSpace(lines[i-1])
			if isHeadingLike(trimmed) || looksLikeSignature(trimmed) {
				anchors = append(anchors, i)
			}
			if isCommentLine(trimmed) != isCommentLine(prevTrimmed) {
				anchors = append(anchors, i)
			}
		}

		prevDepth := braceDepth
		braceDepth += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
		if braceDepth < 0 {
			braceDepth = 0
		}
		if prevDepth > 0 && braceDepth == 0 && i < end {
			anchors = append(anchors, i+1)
		}
	}

	return dedupeAnchors(start, end, anchors)
}

func dedupeAnchors(start, end int, anchors []int) []int {
	if len(anchors) == 0 {
		return nil
	}

	var deduped []int
	last := -1
	for _, anchor := range anchors {
		if anchor <= start || anchor > end {
			continue
		}
		if anchor == last {
			continue
		}
		if len(deduped) > 0 && anchor-deduped[len(deduped)-1] <= 1 {
			continue
		}
		deduped = append(deduped, anchor)
		last = anchor
	}
	return deduped
}

func classifyBlockKind(lines []string, start, end int) string {
	first := strings.TrimSpace(lines[start])
	if isHeadingLike(first) {
		return "heading"
	}
	if looksLikeSignature(first) {
		return "signature"
	}
	allComments := true
	for i := start; i <= end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if !isCommentLine(trimmed) {
			allComments = false
			break
		}
	}
	if allComments {
		return "comment"
	}
	return "fallback"
}

func isHeadingLike(trimmed string) bool {
	if isATXHeading(trimmed) {
		return true
	}
	if len(trimmed) > 0 && len(trimmed) <= 80 &&
		strings.HasSuffix(trimmed, ":") &&
		!strings.ContainsAny(trimmed, "{}();") &&
		!looksLikeSignature(trimmed) {
		return true
	}
	return false
}

func isCommentLine(trimmed string) bool {
	switch {
	case strings.HasPrefix(trimmed, "//"):
		return true
	case strings.HasPrefix(trimmed, "#"):
		return true
	case strings.HasPrefix(trimmed, "--"):
		return true
	case strings.HasPrefix(trimmed, ";"):
		return true
	case strings.HasPrefix(trimmed, "/*"):
		return true
	case strings.HasPrefix(trimmed, "*/"):
		return true
	case strings.HasPrefix(trimmed, "*"):
		return true
	default:
		return false
	}
}

func looksLikeSignature(trimmed string) bool {
	if trimmed == "" || isCommentLine(trimmed) {
		return false
	}

	lower := strings.ToLower(trimmed)
	signaturePrefixes := []string{
		"func ", "fn ", "function ", "def ", "class ", "interface ", "struct ",
		"enum ", "impl ", "type ", "module ", "sub ", "proc ",
		"public ", "private ", "protected ",
	}
	for _, prefix := range signaturePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	if strings.HasSuffix(trimmed, "{") && strings.Contains(trimmed, "(") {
		return true
	}
	if strings.HasSuffix(trimmed, ":") && (strings.HasPrefix(lower, "def ") ||
		strings.HasPrefix(lower, "class ") || strings.HasPrefix(lower, "module ")) {
		return true
	}
	if strings.Contains(trimmed, "=>") && strings.Contains(trimmed, "=") {
		return true
	}

	return false
}

func buildLinePrefix(lines []string) []int {
	prefix := make([]int, len(lines)+1)
	for i := range lines {
		prefix[i+1] = prefix[i] + len(lines[i])
	}
	return prefix
}

func spanChars(prefix []int, start, end int) int {
	if start > end || start < 0 || end+1 >= len(prefix) {
		return 0
	}
	// +newlines between lines in the joined text
	return prefix[end+1] - prefix[start] + (end - start)
}
