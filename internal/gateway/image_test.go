package gateway

import (
	"reflect"
	"testing"
)

func imageIDs(t *testing.T, body string) []string {
	t.Helper()
	models, err := ParseModels([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return ModelIDs(FilterImages(models))
}

func TestFilterImagesKnownIDsWithoutMetadata(t *testing.T) {
	got := imageIDs(t, `{"data":[
		{"id":"gpt-5.4"},{"id":"grok-4"},{"id":"gpt-image-2"},{"id":"claude-sonnet-4-6"},
		{"id":"grok-3-image"},{"id":"sora-2"},{"id":"veo-3"},{"id":"gemini-3-pro-image-preview"},
		{"id":"nano-banana-pro"},{"id":"gpt-image-2.6-flare"},{"id":"doubao-seedream-5-0-260128"},
		{"id":"doubao-seedance-1-0-pro"},{"id":"mj_imagine"},{"id":"flux-kontext-pro"},
		{"id":"grok-2-image"},{"id":"grok-flare"},{"id":"my-image-model"},{"id":"text-embedding-3-small"},
		{"id":"gpt-image-1"},{"id":"gpt-image-2"}]}`)
	want := []string{"gpt-image-2.6-flare", "gpt-image-2", "grok-3-image", "doubao-seedream-5-0-260128", "flux-kontext-pro", "gpt-image-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestFilterImagesNoFlareMeansNoFlare(t *testing.T) {
	got := imageIDs(t, `{"data":[{"id":"gpt-4o"},{"id":"gpt-image-1.5"},{"id":"gpt-image-2"}]}`)
	if !reflect.DeepEqual(got, []string{"gpt-image-1.5", "gpt-image-2"}) {
		t.Fatalf("got %v", got)
	}
	if got := imageIDs(t, `{"data":[{"id":"gpt-5.4"},{"id":"grok-4"},{"id":"sora-2"}]}`); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if got := imageIDs(t, `{"data":[]}`); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestFilterImagesMetadata(t *testing.T) {
	got := imageIDs(t, `{"data":[
		{"id":"vendor-painter-x","supported_endpoint_types":["image-generation"]},
		{"id":"vendor-chat","supported_endpoint_types":["openai"],"type":"chat"},
		{"id":"gpt-image-2","type":"chat","output_modalities":["text"]},
		{"id":"gpt-image-1","supported_endpoint_types":["openai-video"]},
		{"id":"gpt-image-1.5","supported_endpoint_types":["openai"]},
		{"id":"vendor-paint-only-meta","output_modalities":["image"]},
		{"id":"grok-3-image","supported_endpoint_types":["gemini"],"output_modalities":["image"]},
		{"id":"flux-kontext-max","supported_endpoint_types":["replicate"]},
		{"id":"vendor-mj","supported_endpoint_types":["mj"],"type":"image"},
		{"id":"vendor-gc","supported_generation_methods":["generateContent"],"output_modalities":["image"]},
		{"id":"gemini-2.5-flash-image","supported_endpoint_types":["image-generation"]},
		{"id":"vendor-video","supported_endpoint_types":["image-generation"],"type":"video"},
		{"id":"doubao-seededit-3-0-i2i-250628","architecture":{"modality":"text+image->image"}},
		{"id":"vendor-vision","architecture":{"modality":"text+image->text"}}
	]}`)
	want := []string{"vendor-painter-x", "gpt-image-1.5", "grok-3-image", "flux-kontext-max", "doubao-seededit-3-0-i2i-250628"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

// TestFilterImagesMockCatalog is one catalog per family: documented Images
// ids are kept without metadata and are not hidden by a foreign endpoint
// type; undocumented or other-protocol ids need Images endpoint metadata,
// and some stay out even with it.
func TestFilterImagesMockCatalog(t *testing.T) {
	got := imageIDs(t, `{"data":[
		{"id":"gpt-5.4"},
		{"id":"gpt-image-2.5-flare"},{"id":"gpt-image-2.5-sunburst"},{"id":"gpt-image-2.6-flare"},
		{"id":"gpt-image-3-preview"},{"id":"gpt-image-"},
		{"id":"grok-3-image"},{"id":"grok-2-image"},{"id":"grok-flare"},{"id":"grok-sunburst"},{"id":"grok-imagine-video"},
		{"id":"flux-kontext-pro"},{"id":"flux-kontext-max"},{"id":"flux-kontext-dev"},
		{"id":"doubao-seedream-4-0-250828"},{"id":"doubao-seededit-3-0-i2i-250628","architecture":{"modality":"text+image->image"}},
		{"id":"dall-e-3"},{"id":"dall-e-2"},
		{"id":"gemini-2.5-flash-image","supported_endpoint_types":["image-generation"]},
		{"id":"nano-banana-pro","supported_endpoint_types":["image-generation"]},
		{"id":"midjourney-v7","supported_endpoint_types":["image-generation"]},{"id":"mj_imagine"},
		{"id":"imagen-4.0-generate-001"},{"id":"sora-2"},{"id":"veo-3"},{"id":"doubao-seedance-1-0-pro"},
		{"id":"my-image-model"},{"id":"gpt-image-chat","type":"chat","output_modalities":["text"]},
		{"id":"vendor-a","supported_endpoint_types":["image-generation"]},
		{"id":"vendor-b","supported_endpoint_types":["/v1/images/generations"]},
		{"id":"vendor-c","supported_endpoint_types":["images/edits"]},
		{"id":"vendor-d","supported_endpoint_types":["image_edit"]},
		{"id":"vendor-e","supported_endpoint_types":["images"]},
		{"id":"vendor-f","output_modalities":["image"]}
	]}`)
	want := []string{
		"gpt-image-2.6-flare", "gpt-image-2.5-flare", "gpt-image-2.5-sunburst", "gpt-image-3-preview",
		"grok-3-image", "flux-kontext-pro", "flux-kontext-max",
		"doubao-seedream-4-0-250828", "doubao-seededit-3-0-i2i-250628", "dall-e-3", "dall-e-2",
		"vendor-a", "vendor-b", "vendor-c", "vendor-d", "vendor-e",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}

	// The live v0.3.19 picker: 2.5 ids are now kept by id, flare first;
	// without flare in the catalog it is not invented.
	got = imageIDs(t, `{"data":[{"id":"gpt-image-2.5-flare"},{"id":"gpt-image-2.5-sunburst"}]}`)
	if !reflect.DeepEqual(got, []string{"gpt-image-2.5-flare", "gpt-image-2.5-sunburst"}) {
		t.Fatalf("got %v", got)
	}

	// Endpoint metadata may add an undocumented id such as flux-kontext-dev.
	got = imageIDs(t, `{"data":[{"id":"flux-kontext-dev","supported_endpoint_types":["image-generation"]},{"id":"grok-2-image","supported_endpoint_types":["images/generations"]}]}`)
	if !reflect.DeepEqual(got, []string{"flux-kontext-dev", "grok-2-image"}) {
		t.Fatalf("got %v", got)
	}

	// Exclusions still win over a documented id.
	got = imageIDs(t, `{"data":[
		{"id":"grok-3-image","supported_endpoint_types":["openai-video"]},
		{"id":"dall-e-3","type":"chat","output_modalities":["text"]},
		{"id":"gpt-image-2.5-flare","type":"embedding"}
	]}`)
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestFilterImagesDropsDuplicatesAndBlanks(t *testing.T) {
	models := []Model{{ID: ""}, {ID: "gpt-image-2"}, {ID: "gpt-image-2"}, {ID: "doubao-seedream-"}, {ID: "gpt-image-"}}
	if got := ModelIDs(FilterImages(models)); !reflect.DeepEqual(got, []string{"gpt-image-2"}) {
		t.Fatalf("got %v", got)
	}
}
