package provider

import "testing"

func TestPresets(t *testing.T) {
	if Default().ID != "deeprouter" || Default().BaseURL != "https://deeprouter.top" {
		t.Fatalf("default %+v", Default())
	}
	all := All()
	if len(all) != 3 {
		t.Fatalf("%d presets", len(all))
	}
	wolf, ok := ByID(" WolfAI ")
	if !ok || wolf.BaseURL != "https://wolfai.top" || wolf.Name != "WolfAI" {
		t.Fatalf("%+v %v", wolf, ok)
	}
	link, ok := ByID("linkmesh")
	if !ok || link.BaseURL != "https://www.linkmesh.org" {
		t.Fatalf("%+v", link)
	}
	if _, err := Parse("custom"); err == nil {
		t.Fatal("custom is not a --provider value")
	}
	if _, err := Parse("nope"); err == nil {
		t.Fatal("accepted unknown provider")
	}
	if NormalizeID("") != "deeprouter" || NormalizeID("custom") != "custom" || NormalizeID("LINKMESH") != "linkmesh" {
		t.Fatal(NormalizeID(""), NormalizeID("custom"), NormalizeID("LINKMESH"))
	}
	if DisplayName("wolfai") != "WolfAI" || DisplayName("custom") != "Custom" || DisplayName("") != "DeepRouter" {
		t.Fatal(DisplayName("wolfai"), DisplayName("custom"), DisplayName(""))
	}
	if !KnownLabel("") || !KnownLabel("DeepRouter") || !KnownLabel("wolfai") || !KnownLabel("Custom") || KnownLabel("Azure") {
		t.Fatal("known label")
	}
	ids := ManagedIDs()
	if len(ids) != 4 || ids[3] != "custom" || !IsManaged("wolfai") || IsManaged("openai") {
		t.Fatalf("%v", ids)
	}
}
