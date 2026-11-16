package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/llm"
	"github.com/HuskerMinion/techo5/echod/internal/lib/netguard"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

const (
	hindsightRecallTimeout = 1500 * time.Millisecond
	hindsightRetainTimeout = 4 * time.Second
)

func hindsightHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:       nil,
			DialContext: netguard.LocalOnlyDialer(timeout).DialContext,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

type hindsightMemoryEntry struct {
	Text    string `json:"text"`
	Content string `json:"content"`
}

type hindsightRecallResponse struct {
	Items   []hindsightMemoryEntry `json:"items"`
	Results []hindsightMemoryEntry `json:"results"`
}

var hindsightStopwords = map[string]bool{
	"what": true, "who": true, "where": true, "when": true, "why": true, "how": true,
	"are": true, "you": true, "your": true, "the": true, "and": true, "for": true,
	"with": true, "that": true, "this": true, "from": true, "have": true, "has": true,
	"can": true, "tell": true, "about": true, "hello": true, "hey": true, "reachy": true,
	"mini": true, "please": true, "know": true, "remember": true, "memories": true,
}

func extractSearchKeyword(query string) string {
	words := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	best := ""
	for _, w := range words {
		lw := strings.ToLower(w)
		if len(lw) < 3 || hindsightStopwords[lw] {
			continue
		}
		if len(w) > len(best) {
			best = w
		}
	}
	return best
}

func parseHindsightEntries(resp *http.Response) []string {
	var parsed hindsightRecallResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil
	}
	entries := append(parsed.Items, parsed.Results...)
	var out []string
	for _, r := range entries {
		txt := strings.TrimSpace(r.Text)
		if txt == "" {
			txt = strings.TrimSpace(r.Content)
		}
		if txt == "" {
			continue
		}
		if idx := strings.Index(txt, " | When:"); idx > 0 {
			txt = strings.TrimSpace(txt[:idx])
		}
		if len(txt) > 280 {
			txt = txt[:280] + "..."
		}
		out = append(out, txt)
	}
	return out
}

func listFromBank(ctx context.Context, client *http.Client, base, bank, q string, limit int) ([]string, bool) {
	u := fmt.Sprintf("%s/banks/%s/memories/list?limit=%d", base, bank, limit)
	if q != "" {
		u += "&q=" + url.QueryEscape(q)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	return parseHindsightEntries(resp), true
}

func recallFromBank(ctx context.Context, base, bank, query string, limit int) []string {
	if base == "" || bank == "" || strings.TrimSpace(query) == "" {
		return nil
	}
	client := hindsightHTTPClient(hindsightRecallTimeout)

	// Fast path (<60 ms): query Hindsight's indexed memories/list endpoint by keyword, falling back
	// to most-recent bank memories if the keyword has no exact substring match.
	kw := extractSearchKeyword(query)
	if kw != "" {
		if items, ok := listFromBank(ctx, client, base, bank, kw, limit); ok && len(items) > 0 {
			return items
		}
	}
	if items, ok := listFromBank(ctx, client, base, bank, "", limit); ok && len(items) > 0 {
		return items
	}

	// Fallback: POST bank query endpoint with budget=low
	reqBody, err := json.Marshal(map[string]any{
		"query":      query,
		"budget":     "low",
		"max_tokens": 256,
		"types":      []string{"world", "experience"},
	})
	if err != nil {
		return nil
	}
	u := fmt.Sprintf("%s/banks/%s/%s/%s", base, bank, "memories", "recall")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(reqBody))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	return parseHindsightEntries(resp)
}

func recallHindsightContext(ctx context.Context, b config.Brain, query string) string {
	base := b.EffectiveHindsight()
	if base == "" || strings.TrimSpace(query) == "" {
		return ""
	}
	rctx, cancel := context.WithTimeout(ctx, hindsightRecallTimeout)
	defer cancel()

	primaryBank := b.EffectiveHindsightBank()
	writeBank := b.EffectiveHindsightWriteBank()

	var facts []string
	seen := map[string]bool{}
	for _, f := range recallFromBank(rctx, base, primaryBank, query, 3) {
		if !seen[f] {
			seen[f] = true
			facts = append(facts, f)
		}
	}
	if writeBank != "" && writeBank != primaryBank {
		for _, f := range recallFromBank(rctx, base, writeBank, query, 2) {
			if !seen[f] {
				seen[f] = true
				facts = append(facts, f)
			}
		}
	}
	if len(facts) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\nRelevant memories recalled from Hindsight memory bank (")
	sb.WriteString(primaryBank)
	sb.WriteString("):\n")
	for _, f := range facts {
		sb.WriteString("- ")
		sb.WriteString(f)
		sb.WriteString("\n")
	}
	return sb.String()
}

func retainHindsightTurnAsync(b config.Brain, heard, reply string) {
	base := b.EffectiveHindsight()
	writeBank := b.EffectiveHindsightWriteBank()
	if base == "" || writeBank == "" || strings.TrimSpace(heard) == "" || strings.TrimSpace(reply) == "" {
		return
	}
	safe.Go("hindsight retain", func() {
		ctx, cancel := context.WithTimeout(context.Background(), hindsightRetainTimeout)
		defer cancel()

		entry := fmt.Sprintf("Reachy Companion voice turn — User said: %q | Reachy answered: %q",
			strings.TrimSpace(heard), strings.TrimSpace(reply))
		payload, err := json.Marshal(map[string]any{
			"async": true,
			"items": []map[string]any{
				{
					"content": entry,
					"context": "reachy_companion",
				},
			},
		})
		if err != nil {
			return
		}
		u := fmt.Sprintf("%s/banks/%s/memories", base, writeBank)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := hindsightHTTPClient(hindsightRetainTimeout).Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	})
}

func hindsightTools() []tool {
	b := config.Get().Brain
	if b.EffectiveHindsight() == "" {
		return nil
	}
	return []tool{
		{
			llm.Tool{
				Name:        "hindsight_recall",
				Description: "Recall knowledge or past memories from the local Hindsight memory bank.",
				Parameters: object(map[string]any{
					"query": str("The topic, question, or keywords to recall from Hindsight memory."),
				}, "query"),
			},
			func(a map[string]any) (string, error) {
				q := argString(a, "query")
				if q == "" {
					return "no query provided", nil
				}
				ctx, cancel := context.WithTimeout(context.Background(), hindsightRecallTimeout)
				defer cancel()
				res := recallHindsightContext(ctx, config.Get().Brain, q)
				if res == "" {
					return "no matching Hindsight memories found", nil
				}
				return strings.TrimSpace(res), nil
			},
		},
	}
}
