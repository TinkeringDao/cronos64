package setup

import (
	"net/url"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func TestSaveBrainPreservesVLMAndVisionModel(t *testing.T) {
	f, c := in(t)

	if err := config.Set().Brain().SetKey("secret-key"); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Brain().SetReplyPlayer("media_player.desk_show"); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Brain().Set(config.Brain{
		Mode:        config.BrainDirect,
		STT:         "10.0.0.20:10300",
		TTS:         "10.0.0.20:10200",
		Voice:       "en_US-lessac-medium",
		Language:    "en",
		LLM:         "http://10.0.0.20:8080/v1",
		Model:       "qwen2.5-7b",
		VLM:         "http://10.0.0.21:8081/v1",
		VisionModel: "qwen2.5-vl-3b",
		Prompt:      "Initial prompt",
	}); err != nil {
		t.Fatal(err)
	}

	to := post(t, f, c, url.Values{
		"what":     {"brain"},
		"tab":      {"sound"},
		"mode":     {string(config.BrainDirect)},
		"stt":      {"10.0.0.20:10300"},
		"tts":      {"10.0.0.20:10200"},
		"voice":    {"en_GB-alan-medium"},
		"language": {"en"},
		"llm":      {"http://10.0.0.20:8080/v1"},
		"model":    {"qwen2.5-7b"},
		"prompt":   {"Updated prompt"},
	})
	if p := to.Query().Get("problem"); p != "" {
		t.Fatalf("saveBrain returned problem: %s", p)
	}

	got := config.Get().Brain
	if got.Voice != "en_GB-alan-medium" || got.Prompt != "Updated prompt" {
		t.Fatalf("updated Voice=%q Prompt=%q, want en_GB-alan-medium and Updated prompt", got.Voice, got.Prompt)
	}
	if got.VLM != "http://10.0.0.21:8081/v1" || got.VisionModel != "qwen2.5-vl-3b" {
		t.Fatalf("VLM=%q VisionModel=%q, want preserved http://10.0.0.21:8081/v1 and qwen2.5-vl-3b", got.VLM, got.VisionModel)
	}
	if got.Key != "secret-key" || got.ReplyPlayer != "media_player.desk_show" {
		t.Fatalf("Key=%q ReplyPlayer=%q, want preserved", got.Key, got.ReplyPlayer)
	}

	// Switching mode via the form must also keep VLM and VisionModel intact.
	to = post(t, f, c, url.Values{
		"what":   {"brain"},
		"tab":    {"sound"},
		"mode":   {string(config.BrainHomeAssistant)},
		"prompt": {"HA mode prompt"},
	})
	if p := to.Query().Get("problem"); p != "" {
		t.Fatalf("saveBrain mode switch returned problem: %s", p)
	}
	got = config.Get().Brain
	if got.Mode != config.BrainHomeAssistant || got.Prompt != "HA mode prompt" {
		t.Fatalf("after mode switch Mode=%q Prompt=%q, want empty and HA mode prompt", got.Mode, got.Prompt)
	}
	if got.VLM != "http://10.0.0.21:8081/v1" || got.VisionModel != "qwen2.5-vl-3b" {
		t.Fatalf("after mode switch VLM=%q VisionModel=%q, want preserved", got.VLM, got.VisionModel)
	}
}
