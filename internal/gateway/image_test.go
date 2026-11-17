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
		{"id":"vendor-mj","supported_endpoint_types":["mj"],"type":"image"},
		{"id":"gemini-2.5-flash-image","supported_endpoint_types":["image-generation"]},
		{"id":"vendor-video","supported_endpoint_types":["image-generation"],"type":"video"},
		{"id":"doubao-seededit-3-0-i2i-250628","architecture":{"modality":"text+image->image"}},
		{"id":"vendor-vision","architecture":{"modality":"text+image->text"}}
	]}`)
	want := []string{"vendor-painter-x", "gpt-image-1.5", "doubao-seededit-3-0-i2i-250628"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestFilterImagesDropsDuplicatesAndBlanks(t *testing.T) {
	models := []Model{{ID: ""}, {ID: "gpt-image-2"}, {ID: "gpt-image-2"}, {ID: "doubao-seedream-"}}
	if got := ModelIDs(FilterImages(models)); !reflect.DeepEqual(got, []string{"gpt-image-2"}) {
		t.Fatalf("got %v", got)
	}
}
