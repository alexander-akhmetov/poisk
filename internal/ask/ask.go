package ask

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/akhmetov/poisk/internal/llm"
	"github.com/akhmetov/poisk/internal/search"
)

type Searchable interface {
	Search(ctx context.Context, query string, topK int, folders []string) ([]search.Result, error)
}

type Asker struct {
	searcher         Searchable
	client           *llm.Client
	maxContextChunks int
	systemPrompt     string
}

func NewAsker(searcher Searchable, client *llm.Client, maxContextChunks int, systemPrompt string) *Asker {
	return &Asker{
		searcher:         searcher,
		client:           client,
		maxContextChunks: maxContextChunks,
		systemPrompt:     systemPrompt,
	}
}

func (a *Asker) Ask(ctx context.Context, question string, folders []string) (string, error) {
	msgs, err := a.buildMessages(ctx, question, folders)
	if err != nil {
		return "", err
	}
	return a.client.Complete(ctx, msgs)
}

func (a *Asker) AskStream(ctx context.Context, question string, folders []string, cb func(string) error) error {
	msgs, err := a.buildMessages(ctx, question, folders)
	if err != nil {
		return err
	}
	return a.client.Stream(ctx, msgs, cb)
}

func (a *Asker) buildMessages(ctx context.Context, question string, folders []string) ([]llm.Message, error) {
	results, err := a.searcher.Search(ctx, question, a.maxContextChunks, folders)
	if err != nil && len(results) == 0 {
		return nil, fmt.Errorf("search: %w", err)
	}
	if err != nil {
		slog.Warn("partial search failure, proceeding with available results", "error", err)
	}

	var msgs []llm.Message

	if a.systemPrompt != "" {
		msgs = append(msgs, llm.Message{Role: "system", Content: a.systemPrompt})
	}

	var userContent strings.Builder
	if len(results) > 0 {
		userContent.WriteString("Context:\n\n")
		for _, r := range results {
			loc := fmt.Sprintf("%s:%d", r.FilePath, r.LineNum)
			if r.EndLine > 0 && r.EndLine != r.LineNum {
				loc = fmt.Sprintf("%s:%d-%d", r.FilePath, r.LineNum, r.EndLine)
			}
			meta := ""
			if r.Symbol != "" {
				meta = fmt.Sprintf(" [%s]", r.Symbol)
			}
			fmt.Fprintf(&userContent, "%s%s\n%s\n\n", loc, meta, r.Text)
		}
	}
	userContent.WriteString("Question: ")
	userContent.WriteString(question)

	msgs = append(msgs, llm.Message{Role: "user", Content: userContent.String()})
	return msgs, nil
}
