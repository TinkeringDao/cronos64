package assistant

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/mute"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/camera"
	"github.com/HuskerMinion/techo5/echod/internal/lib/llm"
	"github.com/HuskerMinion/techo5/echod/internal/lib/netguard"
)

const (
	lookDialTimeout = 15 * time.Second
	lookTimeout     = 30 * time.Second
	lookMaxAnswer   = 1 << 20
)

var errBlankSnapshot = errors.New("camera snapshot is blank")

var lookHTTPClient = &http.Client{
	Timeout: lookTimeout,
	Transport: &http.Transport{
		Proxy:               nil,
		DialContext:         netguard.LocalOnlyDialer(lookDialTimeout).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     30 * time.Second,
	},
}

var (
	hasCamera = camera.Available

	coveredOrMuted = func() bool {
		muted, err := mute.Get().Muted()
		return err != nil || muted || lensCovered()
	}

	snapshotJPEG = func(ctx context.Context) ([]byte, error) {
		fr, err := camera.Get().Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		if fr == nil || fr.Blank() {
			return nil, errBlankSnapshot
		}
		img := fr.Image()
		if img == nil {
			return nil, errBlankSnapshot
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
)

func lookTools() []tool {
	if config.Get().Brain.EffectiveVLM() == "" || !hasCamera() {
		return nil
	}
	return []tool{lookTool()}
}

func lookTool() tool {
	return tool{
		Tool: llm.Tool{
			Name:        "look",
			Description: "Take a live camera snapshot to see what is in front of the device and answer a visual question about it.",
			Parameters: object(map[string]any{
				"question": str("What to look for or describe in front of the camera."),
			}, "question"),
		},
		Run: runLook,
	}
}

func runLook(args map[string]any) (string, error) {
	if coveredOrMuted() {
		return "camera is covered or muted", nil
	}
	b := config.Get().Brain
	vlmBase := b.EffectiveVLM()
	if vlmBase == "" {
		return "", errors.New("no local VLM endpoint configured")
	}

	question := argString(args, "question")
	if question == "" {
		question = "What is in front of the camera?"
	}

	ctx, cancel := context.WithTimeout(context.Background(), lookTimeout)
	defer cancel()

	jpg, err := snapshotJPEG(ctx)
	if err != nil {
		if isCameraOff(err) || errors.Is(err, errBlankSnapshot) {
			return "camera is covered or muted", nil
		}
		return "", err
	}
	if len(jpg) == 0 {
		return "", errors.New("camera returned an empty snapshot")
	}

	dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpg)
	reqPayload := map[string]any{
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": "Describe concisely and accurately what is visible in the camera snapshot to answer the user's question in one or two plain spoken sentences.",
			},
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "text",
						"text": question,
					},
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": dataURL,
						},
					},
				},
			},
		},
		"temperature": 0.2,
		"max_tokens":  256,
	}
	if model := b.EffectiveVisionModel(); model != "" {
		reqPayload["model"] = model
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, vlmEndpoint(vlmBase), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if b.Key != "" {
		req.Header.Set("Authorization", "Bearer "+b.Key)
	}

	resp, err := lookHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, lookMaxAnswer))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vlm: %s: %.200s", resp.Status, strings.TrimSpace(string(respBytes)))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("vlm: invalid JSON response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("vlm: empty choices")
	}
	text := extractVLMContent(parsed.Choices[0].Message.Content)
	if text == "" {
		return "", errors.New("vlm: empty description")
	}
	return text, nil
}

func vlmEndpoint(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	if u, err := url.Parse(base); err == nil && u.Path == "" {
		base += "/v1"
	}
	return base + "/chat/completions"
}

func extractVLMContent(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var out []string
		for _, p := range parts {
			if t := strings.TrimSpace(p.Text); t != "" {
				out = append(out, t)
			}
		}
		return strings.Join(out, " ")
	}
	return ""
}
