package config

import (
	"path/filepath"
	"testing"
)

func TestBrainEffectiveVLMAndVisionModel(t *testing.T) {
	var empty Brain
	if got := empty.EffectiveVLM(); got != "" {
		t.Fatalf("empty EffectiveVLM() = %q, want empty", got)
	}
	if got := empty.EffectiveVisionModel(); got != "" {
		t.Fatalf("empty EffectiveVisionModel() = %q, want empty", got)
	}

	fallback := Brain{
		LLM:   "http://10.0.0.20:8080/v1",
		Model: "qwen2.5-7b",
	}
	if got := fallback.EffectiveVLM(); got != "http://10.0.0.20:8080/v1" {
		t.Fatalf("fallback EffectiveVLM() = %q, want LLM base", got)
	}
	if got := fallback.EffectiveVisionModel(); got != "qwen2.5-7b" {
		t.Fatalf("fallback EffectiveVisionModel() = %q, want Model", got)
	}

	whitespaceOverride := Brain{
		LLM:         "http://10.0.0.20:8080/v1",
		Model:       "qwen2.5-7b",
		VLM:         "   ",
		VisionModel: "\t\n",
	}
	if got := whitespaceOverride.EffectiveVLM(); got != "http://10.0.0.20:8080/v1" {
		t.Fatalf("whitespace VLM EffectiveVLM() = %q, want LLM fallback", got)
	}
	if got := whitespaceOverride.EffectiveVisionModel(); got != "qwen2.5-7b" {
		t.Fatalf("whitespace VisionModel EffectiveVisionModel() = %q, want Model fallback", got)
	}

	dedicated := Brain{
		LLM:         "http://10.0.0.20:8080/v1",
		Model:       "qwen2.5-7b",
		VLM:         " http://10.0.0.21:8081/v1 ",
		VisionModel: " qwen2.5-vl-3b ",
	}
	if got := dedicated.EffectiveVLM(); got != "http://10.0.0.21:8081/v1" {
		t.Fatalf("dedicated EffectiveVLM() = %q, want trimmed VLM", got)
	}
	if got := dedicated.EffectiveVisionModel(); got != "qwen2.5-vl-3b" {
		t.Fatalf("dedicated EffectiveVisionModel() = %q, want trimmed VisionModel", got)
	}
}

func TestBrainWriterSetPreservesVLMAndVisionModel(t *testing.T) {
	Use(filepath.Join(t.TempDir(), "state.json"))
	if err := Set().Brain().SetKey("secret-key"); err != nil {
		t.Fatal(err)
	}
	if err := Set().Brain().SetReplyPlayer("media_player.desk_show"); err != nil {
		t.Fatal(err)
	}
	want := Brain{
		Mode:        BrainDirect,
		STT:         "10.0.0.20:10300",
		TTS:         "10.0.0.20:10200",
		LLM:         "http://10.0.0.20:8080/v1",
		Model:       "qwen2.5-7b",
		VLM:         "http://10.0.0.21:8081/v1",
		VisionModel: "qwen2.5-vl-3b",
	}
	if err := Set().Brain().Set(want); err != nil {
		t.Fatal(err)
	}
	got := Get().Brain
	if got.VLM != want.VLM || got.VisionModel != want.VisionModel {
		t.Fatalf("saved VLM=%q VisionModel=%q, want %q and %q", got.VLM, got.VisionModel, want.VLM, want.VisionModel)
	}
	if got.Key != "secret-key" || got.ReplyPlayer != "media_player.desk_show" {
		t.Fatalf("Key=%q ReplyPlayer=%q, want preserved", got.Key, got.ReplyPlayer)
	}
}
