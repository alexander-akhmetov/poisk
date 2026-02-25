package search

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/akhmetov/poisk/internal/llm"
)

type rerankBlendConfig struct {
	TopRetrievalWeight    float64
	BottomRetrievalWeight float64
}

const rerankPromptTemplate = `Rate the relevance of each document to the query on a 0-10 scale.

Query: QUERY_PLACEHOLDER

Documents:
DOCS_PLACEHOLDER
Return ONLY JSON in one of these forms:
1) [8,3,7,5]
2) {"scores":[8,3,7,5]}
One score per document, in order. No prose, no extra keys.`

var numericTokenPattern = regexp.MustCompile(`-?\d+(?:\.\d+)?`)
var indexedScorePattern = regexp.MustCompile(`(?m)(?:^|[\n,;])\s*(?:\[\s*)?\d+(?:\s*\])?\s*[:\-\)]\s*(-?\d+(?:\.\d+)?)`)
var rangeHintPattern = regexp.MustCompile(`-?\d+(?:\.\d+)?\s*-\s*-?\d+(?:\.\d+)?`)
var scoreAfterSeparatorPattern = regexp.MustCompile(`[:=]\s*(-?\d+(?:\.\d+)?)`)
var countPrefixPattern = regexp.MustCompile(`(?i)^\s*\d+(?:\.\d+)?\s+(?:doc|docs|document|documents|item|items|result|results)\b`)
var numberedCSVItemPattern = regexp.MustCompile(`^\s*\d+\s*[\.\):\-]\s*(-?\d+(?:\.\d+)?)`)

func rerankResults(ctx context.Context, client *llm.Client, query string, results []Result, topN int, blendCfg rerankBlendConfig) []Result {
	if len(results) == 0 {
		return results
	}
	if topN <= 0 || topN > len(results) {
		topN = len(results)
	}

	// Only rerank top N candidates
	candidates := results
	if len(candidates) > topN {
		candidates = candidates[:topN]
	}

	var docs strings.Builder
	for i, r := range candidates {
		text := truncateRunes(r.Text, 500)
		fmt.Fprintf(&docs, "[%d] %s:%d %s\n", i+1, r.FilePath, r.LineNum, text)
	}

	prompt := strings.NewReplacer("QUERY_PLACEHOLDER", query, "DOCS_PLACEHOLDER", docs.String()).Replace(rerankPromptTemplate)
	resp, err := client.Complete(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	}, 0.0, 100)
	if err != nil {
		slog.Warn("reranking failed, keeping original order", "error", err)
		return results
	}

	scores := parseScores(resp, len(candidates))
	if scores == nil {
		slog.Warn("failed to parse reranker scores, keeping original order", "response", resp)
		return results
	}

	blendCfg = normalizeBlendConfig(blendCfg)

	// Position-aware blending: top results keep more retrieval weight
	for i := range candidates {
		positionWeight := blendCfg.TopRetrievalWeight
		if len(candidates) > 1 {
			progress := float64(i) / float64(len(candidates)-1)
			span := blendCfg.TopRetrievalWeight - blendCfg.BottomRetrievalWeight
			positionWeight = blendCfg.TopRetrievalWeight - span*progress
		}
		rerankWeight := 1.0 - positionWeight
		candidates[i].Score = positionWeight*candidates[i].Score + rerankWeight*(scores[i]/10.0)
	}

	// Re-merge reranked candidates with remaining results
	reranked := make([]Result, 0, len(results))
	reranked = append(reranked, candidates...)
	if len(results) > topN {
		reranked = append(reranked, results[topN:]...)
	}

	// Stable sort keeps deterministic ordering when scores tie.
	sort.SliceStable(reranked, func(i, j int) bool { return reranked[i].Score > reranked[j].Score })

	return reranked
}

func parseScores(resp string, expected int) []float64 {
	if expected <= 0 {
		return nil
	}
	resp = strings.TrimSpace(resp)
	if resp == "" {
		return nil
	}

	if scores := parseScoresFromJSON(resp, expected); scores != nil {
		return scores
	}

	if scores := parseScoresFromIndexedText(resp, expected); scores != nil {
		return scores
	}

	if scores := parseScoresFromCSV(resp, expected); scores != nil {
		return scores
	}

	return parseScoresFromNumericFallback(resp, expected)
}

func normalizeBlendConfig(cfg rerankBlendConfig) rerankBlendConfig {
	top := sanitizeWeight(cfg.TopRetrievalWeight, 0.8)
	bottom := sanitizeWeight(cfg.BottomRetrievalWeight, 0.2)
	if top < bottom {
		top, bottom = bottom, top
	}
	return rerankBlendConfig{
		TopRetrievalWeight:    top,
		BottomRetrievalWeight: bottom,
	}
}

func sanitizeWeight(v, fallback float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fallback
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func parseScoresFromJSON(resp string, expected int) []float64 {
	candidates := []string{
		resp,
		stripCodeFence(resp),
		extractJSONRange(resp, '[', ']'),
		extractJSONRange(resp, '{', '}'),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if scores := decodeJSONScores(candidate); scores != nil {
			return clampAndPick(scores, expected)
		}
	}
	return nil
}

func decodeJSONScores(candidate string) []float64 {
	var arr []float64
	if err := json.Unmarshal([]byte(candidate), &arr); err == nil && len(arr) > 0 {
		return arr
	}

	var obj struct {
		Scores          []float64 `json:"scores"`
		RelevanceScores []float64 `json:"relevance_scores"`
		Ratings         []float64 `json:"ratings"`
	}
	if err := json.Unmarshal([]byte(candidate), &obj); err != nil {
		return nil
	}
	switch {
	case len(obj.Scores) > 0:
		return obj.Scores
	case len(obj.RelevanceScores) > 0:
		return obj.RelevanceScores
	case len(obj.Ratings) > 0:
		return obj.Ratings
	default:
		return nil
	}
}

func parseScoresFromIndexedText(resp string, expected int) []float64 {
	matches := indexedScorePattern.FindAllStringSubmatch(resp, -1)
	if len(matches) == 0 {
		return nil
	}
	values := make([]float64, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		score, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		values = append(values, score)
	}
	return clampAndPick(values, expected)
}

func parseScoresFromCSV(resp string, expected int) []float64 {
	if !strings.Contains(resp, ",") {
		return nil
	}
	parts := strings.Split(resp, ",")
	values := make([]float64, 0, len(parts))
	for _, part := range parts {
		nums := numericTokenPattern.FindAllString(part, -1)
		if len(nums) == 0 {
			continue
		}
		token := nums[len(nums)-1]
		match := scoreAfterSeparatorPattern.FindStringSubmatch(part)
		if numbered := numberedCSVItemPattern.FindStringSubmatch(part); len(numbered) > 1 {
			token = numbered[1]
		} else if segmentStartsWithNumber(part) {
			token = nums[0]
			if len(match) > 1 && countPrefixPattern.MatchString(part) {
				token = match[1]
			}
		} else if len(match) > 1 {
			token = match[1]
		}
		score, err := strconv.ParseFloat(token, 64)
		if err != nil {
			continue
		}
		values = append(values, score)
	}
	return clampAndPick(values, expected)
}

func parseScoresFromNumericFallback(resp string, expected int) []float64 {
	if idx := strings.LastIndex(resp, ":"); idx != -1 {
		tail := strings.TrimSpace(resp[idx+1:])
		if tail != "" {
			if scores := parseScoresFromNumericTokens(tail, expected); scores != nil {
				return scores
			}
		}
	}
	return parseScoresFromNumericTokens(resp, expected)
}

func parseScoresFromNumericTokens(resp string, expected int) []float64 {
	resp = rangeHintPattern.ReplaceAllString(resp, " ")
	nums := numericTokenPattern.FindAllString(resp, -1)
	if len(nums) < expected {
		return nil
	}
	values := make([]float64, 0, len(nums))
	for _, token := range nums {
		score, err := strconv.ParseFloat(token, 64)
		if err != nil {
			continue
		}
		values = append(values, score)
	}
	if len(values) < expected {
		return nil
	}
	if expected == 1 {
		return clampAndPick(values, expected)
	}
	if len(values) == expected && looksLikeOrdinalPrefix(values, expected) {
		return nil
	}
	if len(values) == 2*expected && looksLikeIndexedPairs(values) {
		pairs := make([]float64, 0, expected)
		for i := 1; i < len(values); i += 2 {
			pairs = append(pairs, values[i])
		}
		return clampAndPick(pairs, expected)
	}
	if len(values) > expected && looksLikeOrdinalPrefix(values, expected) {
		remaining := values[expected:]
		if len(remaining) >= expected {
			return clampAndPick(remaining, expected)
		}
		return nil
	}
	return clampAndPick(values, expected)
}

func looksLikeIndexedPairs(values []float64) bool {
	for i := 0; i < len(values); i += 2 {
		expectedIndex := float64(i/2 + 1)
		if math.Abs(values[i]-expectedIndex) > 1e-6 {
			return false
		}
	}
	return true
}

func looksLikeOrdinalPrefix(values []float64, expected int) bool {
	if len(values) < expected {
		return false
	}
	for i := range expected {
		expectedIndex := float64(i + 1)
		if math.Abs(values[i]-expectedIndex) > 1e-6 {
			return false
		}
	}
	return true
}

func clampAndPick(values []float64, expected int) []float64 {
	if len(values) < expected {
		return nil
	}
	scores := make([]float64, expected)
	for i := range expected {
		score := values[i]
		if score < 0 {
			score = 0
		}
		if score > 10 {
			score = 10
		}
		scores[i] = score
	}
	return scores
}

func segmentStartsWithNumber(part string) bool {
	part = strings.TrimSpace(part)
	if part == "" {
		return false
	}
	ch := part[0]
	return ch == '-' || ch == '+' || (ch >= '0' && ch <= '9')
}

func stripCodeFence(resp string) string {
	resp = strings.TrimSpace(resp)
	if !strings.HasPrefix(resp, "```") {
		return resp
	}
	lines := strings.Split(resp, "\n")
	if len(lines) < 3 {
		return resp
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		return resp
	}
	last := len(lines) - 1
	if !strings.HasPrefix(strings.TrimSpace(lines[last]), "```") {
		return resp
	}
	return strings.TrimSpace(strings.Join(lines[1:last], "\n"))
}

func extractJSONRange(resp string, open, closeByte byte) string {
	start := strings.IndexByte(resp, open)
	end := strings.LastIndexByte(resp, closeByte)
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	return strings.TrimSpace(resp[start : end+1])
}

func truncateRunes(s string, maxRunes int) string {
	count := 0
	for i := range s {
		if count == maxRunes {
			return s[:i] + "..."
		}
		count++
	}
	return s
}
