package gateway

import "strings"

// FlareImageModel is the image Skill's default model. It is not on an
// official API page; it was seen on GET /v1/models for one token, and POST
// /v1/images/generations returned a picture. FilterImages lists it first
// when a catalog has it and never adds it when it does not.
const FlareImageModel = "gpt-image-2.6-flare"

// imagesIDs are ids DeepRouter documents for POST /v1/images/generations or
// /v1/images/edits (checked 2026-11-17 against deeprouter.apifox.cn):
// the GPT-Image generate and edit pages, 图像生成 > Grok "创建 Image" /
// "编辑 image", and "创建 DALL·E 3" (example dall-e-3; the text also names
// dall-e-2). FlareImageModel is the one offline entry.
var imagesIDs = map[string]bool{
	"gpt-image-1":      true,
	"gpt-image-1.5":    true,
	"gpt-image-2":      true,
	"gpt-image-1-all":  true,
	"gpt-image-2-all":  true,
	"flux-kontext-pro": true,
	"flux-kontext-max": true,
	"grok-3-image":     true,
	"dall-e-3":         true,
	"dall-e-2":         true,
	FlareImageModel:    true,
}

// imagesIDPrefixes are families whose official pages are POST
// /v1/images/generations: 图像生成 > Doubao "创建图片" lists
// doubao-seedream-* and doubao-seededit-* ids, and the GPT-Image pages
// document gpt-image-1 / 1.5 / 2 (tokens also list gpt-image-2.5-flare,
// gpt-image-2.5-sunburst, and FlareImageModel in that family).
var imagesIDPrefixes = []string{"gpt-image-", "doubao-seedream-", "doubao-seededit-"}

// notImagesIDTokens are ids DeepRouter documents on another protocol, or
// that are video: Gemini / Nano Banana image is generateContent, Midjourney
// is a task API. They are left out even when metadata says image.
var notImagesIDTokens = []string{"gemini", "nano-banana", "imagen", "midjourney", "mj_", "video", "sora", "veo", "seedance"}

// imageOtherTokens mark a model that is not an image generator.
var imageOtherTokens = []string{"video", "audio", "speech", "tts", "whisper", "transcri", "realtime", "embed", "moderat", "rerank", "sora", "veo"}

// FilterImages keeps the models the image Skill can call: POST
// /v1/images/generations and /v1/images/edits. A model is kept when the
// gateway's endpoint types name the Images API, or when its id is one
// DeepRouter documents on that API. Metadata that says video, audio,
// embedding, moderation, or chat only drops a model. Endpoint types that
// name another protocol (generateContent, a task API) neither add an
// unknown id nor hide a documented one, and an output modality of "image"
// alone does not add one. An id that merely contains "image" is not enough.
// FlareImageModel comes first when present; otherwise the catalog order is
// kept. Being listed says the token can see the id, not that a request will
// be accepted.
func FilterImages(models []Model) []Model {
	var out []Model
	seen := map[string]bool{}
	for _, m := range models {
		if m.ID == "" || seen[m.ID] || !m.IsImages() {
			continue
		}
		seen[m.ID] = true
		if m.ID == FlareImageModel {
			out = append([]Model{m}, out...)
			continue
		}
		out = append(out, m)
	}
	return out
}

// IsImages reports whether m belongs in the image Skill's model picker.
func (m Model) IsImages() bool {
	low := strings.ToLower(strings.TrimSpace(m.ID))
	if low == "" {
		return false
	}
	for _, tok := range notImagesIDTokens {
		if strings.Contains(low, tok) {
			return false
		}
	}
	endpoint, excluded := m.imagesMeta()
	if excluded {
		return false
	}
	return endpoint || imagesID(low)
}

func imagesID(low string) bool {
	if imagesIDs[low] {
		return true
	}
	for _, p := range imagesIDPrefixes {
		if strings.HasPrefix(low, p) && len(low) > len(p) {
			return true
		}
	}
	return false
}

// imagesMeta reads the gateway metadata. endpoint is set when an endpoint
// type names the Images API (new-api's "image-generation", or the path).
// excluded is set when the model is video, audio, embedding, moderation,
// or chat with no image output.
func (m Model) imagesMeta() (endpoint, excluded bool) {
	for _, ep := range m.EndpointTypes {
		low := normMeta(ep)
		if strings.Contains(low, "image-generation") || strings.Contains(low, "images/generations") ||
			strings.Contains(low, "images/edits") || strings.Contains(low, "image-edit") || low == "images" {
			endpoint = true
		}
	}

	imageOut := endpoint
	var nonImage bool
	check := func(s string) {
		side := normMeta(s)
		if i := strings.Index(side, "->"); i >= 0 {
			side = side[i+2:]
		} else if i := strings.Index(side, "-to-"); i >= 0 {
			side = side[i+4:]
		}
		if strings.Contains(side, "image") {
			imageOut = true
		}
		for _, tok := range imageOtherTokens {
			if strings.Contains(side, tok) {
				nonImage = true
			}
		}
	}
	for _, s := range m.Labels {
		check(s)
	}
	if obj := normMeta(m.Object); obj != "" && obj != "model" && obj != "list" {
		check(obj)
	}
	for _, s := range m.OutputModalities {
		check(s)
	}
	for _, s := range m.Capabilities {
		check(s)
	}
	for _, s := range m.EndpointTypes {
		if !providerEndpoint(s) {
			check(s)
		}
	}
	for _, s := range m.Modalities {
		// A bare modality list is often the input side; only an
		// "a->b" entry says what comes out.
		if strings.Contains(s, "->") || strings.Contains(normMeta(s), "-to-") {
			check(s)
		}
	}
	if nonImage {
		return endpoint, true
	}
	if decisive, chat := m.classify(); decisive && chat && !imageOut {
		return endpoint, true
	}
	return endpoint, false
}

func normMeta(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "_", "-")
}
