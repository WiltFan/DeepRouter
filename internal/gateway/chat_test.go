package gateway

import (
	"strings"
	"testing"
)

func TestFilterChatByID(t *testing.T) {
	keep := []string{
		"claude-fable-5", "claude-sonnet-4-6", "claude-opus-4-6",
		"gpt-4.1", "gpt-4o", "gpt-5.4", "grok-4", "gemini-2.5-pro", "gemini-2.0-flash",
		"o1", "o3-mini", "codex-mini",
	}
	drop := []string{
		"dall-e-3", "dalle-3", "gpt-image-1", "sora-2", "veo-3", "imagen-3.0-generate-002",
		"text-embedding-3-small", "tts-1", "gpt-4o-mini-tts", "whisper-1", "gpt-4o-transcribe",
		"gpt-4o-audio-preview", "gpt-4o-realtime-preview", "omni-moderation-latest",
		"grok-2-flare", "grok-sunburst", "nano-banana", "gemini-2.5-flash-image", "video-gen-1",
		"gpt-4o-mini-speech",
	}
	var models []Model
	for _, id := range append(append([]string{}, keep...), drop...) {
		models = append(models, Model{ID: id})
	}
	got := ModelIDs(FilterChat(models))
	if strings.Join(got, ",") != strings.Join(keep, ",") {
		t.Fatalf("kept %v", got)
	}
}

func TestFilterChatPrefersMetadata(t *testing.T) {
	body := []byte(`{"data":[
		{"id":"gpt-4o-image","type":"chat"},
		{"id":"gpt-5.4","type":"image"},
		{"id":"sora-lookalike","architecture":{"modality":"text->text"}},
		{"id":"gpt-4o","architecture":{"modality":"text->image"}},
		{"id":"painter","output_modalities":["image"]},
		{"id":"multi","modalities":["text","image"]},
		{"id":"custom","capabilities":{"image_generation":true}},
		{"id":"vision","capabilities":{"vision":true,"chat":true}},
		{"id":"sora-coder","supported_endpoint_types":["chat"]},
		{"id":"dall-e-3","supported_endpoint_types":["openai"]},
		{"id":"plain","supported_endpoint_types":["openai"]},
		{"id":"embed-only","supportedGenerationMethods":["embedContent"]},
		{"id":"gemini-2.0-flash","supportedGenerationMethods":["generateContent","embedContent"]},
		{"id":"gemini-2.5-flash-image","supportedGenerationMethods":["generateContent"]},
		{"id":"gpt-4o","type":1,"object":"model"},
		{"id":"text-embedding-ada","model_type":"text-embedding"},
		{"id":"image-to-text","modality":"image-to-text"}
	]}`)
	models, err := ParseModels(body)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"gpt-4o-image":           true,
		"gpt-5.4":                false,
		"sora-lookalike":         true,
		"gpt-4o":                 false,
		"painter":                false,
		"multi":                  true,
		"custom":                 false,
		"vision":                 true,
		"sora-coder":             true,
		"dall-e-3":               false,
		"plain":                  true,
		"embed-only":             false,
		"gemini-2.0-flash":       true,
		"gemini-2.5-flash-image": false,
		"text-embedding-ada":     false,
		"image-to-text":          true,
	}
	// Two gpt-4o entries: the image-output one, then the numeric-type one.
	// The numeric type is not metadata, so the id keeps it.
	var numericKept bool
	for _, m := range models {
		chat := m.IsChat()
		if m.ID == "gpt-4o" && len(m.Labels) == 0 && len(m.Modalities) == 0 {
			numericKept = chat
			continue
		}
		if want[m.ID] != chat {
			t.Errorf("%s chat=%v labels=%v modalities=%v outputs=%v caps=%v endpoints=%v methods=%v",
				m.ID, chat, m.Labels, m.Modalities, m.OutputModalities, m.Capabilities, m.EndpointTypes, m.GenerationMethods)
		}
	}
	if !numericKept {
		t.Fatal("numeric type field was treated as a classification")
	}
	parsed, err := ParseModels([]byte(deepRouterModelsBody))
	if err != nil {
		t.Fatal(err)
	}
	if len(FilterChat(parsed)) != len(parsed) {
		t.Fatalf("endpoint families were treated as modalities: %+v", parsed)
	}
}

func TestFamilyIDsAndPickDefaultSkipNonChat(t *testing.T) {
	ids := []string{
		"dall-e-3", "gpt-image-1", "sora-2", "gpt-4o-audio-preview", "gpt-4.1", "gpt-5.4",
		"claude-3-image", "claude-sonnet-4-6", "gemini-2.5-flash-image", "gemini-2.5-pro",
		"grok-flare", "grok-4", "whisper-1",
	}
	if got := PickDefault(ids, "gpt"); got != "gpt-5.4" {
		t.Fatalf("gpt %s", got)
	}
	if got := PickDefault(ids, "claude"); got != "claude-sonnet-4-6" {
		t.Fatalf("claude %s", got)
	}
	if got := PickDefault(ids, "gemini"); got != "gemini-2.5-pro" {
		t.Fatalf("gemini %s", got)
	}
	if got := PickDefault(ids, "grok"); got != "grok-4" {
		t.Fatalf("grok %s", got)
	}
	if got := PickDefault([]string{"gpt-image-1", "dall-e-3"}, "gpt"); got != "gpt-image-1" {
		t.Fatalf("fallback %s", got)
	}
	if got := PickDefault([]string{"dall-e-3", "sora-2"}, "gpt"); got != "" {
		t.Fatalf("no gpt %s", got)
	}
	if got := PickDefault([]string{"gpt-4.1"}, "gpt"); got != "gpt-4.1" {
		t.Fatalf("only gpt-4.1 %s", got)
	}
	if got := PickDefault([]string{"gpt-3.5-turbo", "gpt-4.1"}, "gpt"); got != "gpt-3.5-turbo" {
		t.Fatalf("catalog without gpt-5 %s", got)
	}
	if got := PickDefault([]string{"gpt-4o", "claude-sonnet-4-6"}, "gpt"); got != "gpt-4o" {
		t.Fatalf("catalog without gpt-5 prefers gpt-4o %s", got)
	}

	body := []byte(`{"data":[
		{"id":"gpt-5.4","type":"image"},
		{"id":"gpt-image-keep","type":"chat"},
		{"id":"gpt-4o"},
		{"id":"claude-sonnet-4-6"},
		{"id":"gemini-2.5-pro","model_type":"image"},
		{"id":"gemini-2.0-flash"}
	]}`)
	models, err := ParseModels(body)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(FamilyIDs(models, "gpt"), ","); got != "gpt-image-keep,gpt-4o" {
		t.Fatalf("family %s", got)
	}
	if got := PickDefaultModels(models, "gpt"); got != "gpt-4o" {
		t.Fatalf("metadata gpt %s", got)
	}
	if got := PickDefaultModels(models, "gemini"); got != "gemini-2.0-flash" {
		t.Fatalf("metadata gemini %s", got)
	}
	onlyImage := []Model{{ID: "grok-2-image"}, {ID: "grok-flare"}}
	if got := strings.Join(FamilyIDs(onlyImage, "grok"), ","); got != "grok-2-image,grok-flare" {
		t.Fatalf("empty chat family %s", got)
	}
}
