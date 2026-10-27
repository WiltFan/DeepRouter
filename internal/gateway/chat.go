package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Model is one entry from a gateway model list. Metadata is left empty when
// the gateway did not send it; classification then uses the id.
type Model struct {
	ID                string
	Object            string
	Labels            []string
	Modalities        []string
	OutputModalities  []string
	Capabilities      []string
	EndpointTypes     []string
	GenerationMethods []string
	// Recommended is set when the gateway marks this id as its suggestion.
	Recommended bool
}

// nonChatIDTokens are substrings that mark a non-chat model when the gateway
// does not classify it. Matching is case-insensitive.
var nonChatIDTokens = []string{
	"nano-banana",
	"sunburst",
	"moderation",
	"embedding",
	"realtime",
	"whisper",
	"imagen",
	"dall-e",
	"dalle",
	"transcri",
	"speech",
	"image",
	"video",
	"audio",
	"flare",
	"sora",
	"veo",
	"tts",
}

// FilterChat drops image, video, audio, embedding, speech, and moderation
// models. Type or capability metadata from the gateway wins over the id.
func FilterChat(models []Model) []Model {
	if len(models) == 0 {
		return nil
	}
	out := make([]Model, 0, len(models))
	for _, m := range models {
		if m.ID == "" {
			continue
		}
		if m.IsChat() {
			out = append(out, m)
		}
	}
	return out
}

// IsChat reports whether m belongs in a chat model picker.
func (m Model) IsChat() bool {
	if decisive, chat := m.classify(); decisive {
		return chat
	}
	return chatByID(m.ID)
}

// FamilyIDs is the shared list for one program's model picker.
// FilterChat runs first, then the family needle (gpt, claude, gemini, grok)
// is applied the same way as FilterIDs. If that leaves nothing, the family
// list from before FilterChat is returned.
func FamilyIDs(models []Model, kind string) []string {
	all := FilterIDs(ModelIDs(models), kind)
	chat := FilterIDs(ModelIDs(FilterChat(models)), kind)
	if len(chat) == 0 {
		return all
	}
	return chat
}

// ModelIDs returns the ids in order, skipping blanks.
func ModelIDs(models []Model) []string {
	if models == nil {
		return nil
	}
	out := make([]string, 0, len(models))
	for _, m := range models {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out
}

// chatPool is the id list used when a connectivity check has no chosen model.
// Non-chat ids are removed. If that leaves nothing, the original list is used.
func chatPool(models []Model) []string {
	ids := ModelIDs(FilterChat(models))
	if len(ids) == 0 {
		return ModelIDs(models)
	}
	return ids
}

func modelsFromIDs(ids []string) []Model {
	out := make([]Model, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			out = append(out, Model{ID: id})
		}
	}
	return out
}

func chatByID(id string) bool {
	if id == "" {
		return false
	}
	low := strings.ToLower(id)
	for _, tok := range nonChatIDTokens {
		if strings.Contains(low, tok) {
			return false
		}
	}
	return true
}

func (m Model) classify() (decisive bool, chat bool) {
	var chatSig, nonChatSig bool
	for _, label := range m.Labels {
		c, n := classifyModality(label)
		chatSig = chatSig || c
		nonChatSig = nonChatSig || n
	}
	if obj := strings.ToLower(strings.TrimSpace(m.Object)); obj != "" && obj != "model" && obj != "list" {
		c, n := classifyModality(obj)
		chatSig = chatSig || c
		nonChatSig = nonChatSig || n
	}
	c, n := outputSignals(m.OutputModalities)
	chatSig = chatSig || c
	nonChatSig = nonChatSig || n
	for _, mod := range m.Modalities {
		c, n := classifyModality(mod)
		chatSig = chatSig || c
		nonChatSig = nonChatSig || n
	}
	for _, cap := range m.Capabilities {
		c, n := classifyModality(cap)
		chatSig = chatSig || c
		nonChatSig = nonChatSig || n
	}
	epChat, epNon := endpointSignals(m.EndpointTypes)
	if epNon && !epChat {
		nonChatSig = true
	} else if epChat && !epNon {
		chatSig = true
	}
	genChat, genNon := generationSignals(m.GenerationMethods)
	if genNon && !genChat {
		nonChatSig = true
	}
	if !chatSig && !nonChatSig {
		return false, false
	}
	if chatSig && !nonChatSig {
		return true, true
	}
	if nonChatSig && !chatSig {
		return true, false
	}
	if len(m.Labels) > 0 {
		c, n := classifyModality(m.Labels[0])
		if n && !c {
			return true, false
		}
		if c && !n {
			return true, true
		}
		if n && c {
			return true, false
		}
	}
	if outChat, outNon := outputSignals(m.OutputModalities); outChat && !outNon {
		return true, true
	} else if outNon && !outChat {
		return true, false
	}
	return true, true
}

func outputSignals(mods []string) (chat bool, nonChat bool) {
	if len(mods) == 0 {
		return false, false
	}
	for _, mod := range mods {
		c, n := classifyModality(mod)
		chat = chat || c
		nonChat = nonChat || n
	}
	if chat && nonChat {
		return true, false
	}
	return chat, nonChat
}

func endpointSignals(types []string) (chat bool, nonChat bool) {
	for _, ep := range types {
		if providerEndpoint(ep) {
			continue
		}
		c, n := classifyModality(ep)
		chat = chat || c
		nonChat = nonChat || n
	}
	return chat, nonChat
}

func providerEndpoint(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "openai", "anthropic", "gemini", "google", "xai", "x.ai", "vertex", "azure", "cohere", "mistral", "deepseek", "qwen", "groq":
		return true
	default:
		return false
	}
}

func generationSignals(methods []string) (chat bool, nonChat bool) {
	for _, g := range methods {
		low := strings.ToLower(g)
		low = strings.ReplaceAll(low, "_", "")
		low = strings.ReplaceAll(low, "-", "")
		switch {
		case strings.Contains(low, "generatecontent"), strings.Contains(low, "generatemessage"), strings.Contains(low, "chat"):
			chat = true
		case strings.Contains(low, "embed"), strings.Contains(low, "image"), strings.Contains(low, "video"), strings.Contains(low, "predict"), strings.Contains(low, "moderat"):
			nonChat = true
		}
	}
	return chat, nonChat
}

func classifyModality(s string) (chat bool, nonChat bool) {
	low := strings.ToLower(strings.TrimSpace(s))
	low = strings.ReplaceAll(low, "_", "-")
	if low == "" {
		return false, false
	}
	if i := strings.Index(low, "->"); i >= 0 {
		return classifyOutputSide(low[i+2:])
	}
	if i := strings.Index(low, "-to-"); i >= 0 {
		return classifyOutputSide(low[i+4:])
	}
	if strings.Contains(low, "embed") || strings.Contains(low, "moderat") || strings.Contains(low, "whisper") || strings.Contains(low, "transcri") {
		return false, true
	}
	return labelSignals(low)
}

func classifyOutputSide(s string) (chat bool, nonChat bool) {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '+' || r == ',' || r == '|' || r == ' ' || r == '/'
	})
	if len(parts) == 0 {
		parts = []string{s}
	}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(p, "embed") {
			nonChat = true
			continue
		}
		c, n := labelSignals(p)
		chat = chat || c
		nonChat = nonChat || n
	}
	if chat && nonChat {
		return true, false
	}
	return chat, nonChat
}

func labelSignals(low string) (chat bool, nonChat bool) {
	for _, tok := range nonChatIDTokens {
		if strings.Contains(low, tok) {
			nonChat = true
			break
		}
	}
	for _, tok := range []string{"chat", "text", "llm", "language", "completion", "messages", "instruct"} {
		if strings.Contains(low, tok) {
			chat = true
			break
		}
	}
	return chat, nonChat
}

// ParseModels reads a gateway model list. It accepts {"data":[...]},
// {"models":[...]}, or a raw array. Ids are kept in order.
func ParseModels(body []byte) ([]Model, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return []Model{}, nil
	}
	var wrap struct {
		Data             json.RawMessage `json:"data"`
		Models           json.RawMessage `json:"models"`
		Recommended      json.RawMessage `json:"recommended"`
		RecommendedModel json.RawMessage `json:"recommended_model"`
		DefaultModel     json.RawMessage `json:"default_model"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return parseModelList(body)
	}
	var models []Model
	if raw := bytes.TrimSpace(wrap.Data); len(raw) > 0 && string(raw) != "null" {
		parsed, err := parseModelList(wrap.Data)
		if err != nil {
			return nil, fmt.Errorf("model list data: %w", err)
		}
		models = append(models, parsed...)
	}
	if raw := bytes.TrimSpace(wrap.Models); len(raw) > 0 && string(raw) != "null" {
		parsed, err := parseModelList(wrap.Models)
		if err != nil {
			return nil, fmt.Errorf("model list models: %w", err)
		}
		models = append(models, parsed...)
	}
	if models == nil {
		models = []Model{}
	}
	markRecommended(models, wrap.Recommended, wrap.RecommendedModel, wrap.DefaultModel)
	return models, nil
}

// markRecommended flags ids named by a top-level recommendation field.
// A string, an array of strings, or {"id":"..."} are accepted.
func markRecommended(models []Model, fields ...json.RawMessage) {
	want := map[string]bool{}
	for _, raw := range fields {
		for _, id := range recommendationIDs(raw) {
			want[id] = true
		}
	}
	if len(want) == 0 {
		return
	}
	for i := range models {
		if want[models[i].ID] {
			models[i].Recommended = true
		}
	}
}

func recommendationIDs(raw json.RawMessage) []string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	switch raw[0] {
	case '"':
		if s := rawString(raw); s != "" {
			return []string{s}
		}
	case '[':
		return appendStrings(nil, raw)
	case '{':
		var obj struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &obj) == nil && obj.ID != "" {
			return []string{obj.ID}
		}
	}
	return nil
}

func parseModelList(raw []byte) ([]Model, error) {
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(list))
	for _, item := range list {
		m, err := decodeModel(item)
		if err != nil {
			return nil, err
		}
		if m.ID != "" {
			out = append(out, m)
		}
	}
	return out, nil
}

func decodeModel(raw json.RawMessage) (Model, error) {
	var loose struct {
		ID                              string          `json:"id"`
		Object                          json.RawMessage `json:"object"`
		Type                            json.RawMessage `json:"type"`
		ModelType                       json.RawMessage `json:"model_type"`
		ModelTypeCamel                  json.RawMessage `json:"modelType"`
		Category                        json.RawMessage `json:"category"`
		Kind                            json.RawMessage `json:"kind"`
		Modality                        json.RawMessage `json:"modality"`
		OutputModality                  json.RawMessage `json:"output_modality"`
		Modalities                      json.RawMessage `json:"modalities"`
		OutputModalities                json.RawMessage `json:"output_modalities"`
		OutputModalitiesCamel           json.RawMessage `json:"outputModalities"`
		SupportedModalities             json.RawMessage `json:"supported_modalities"`
		Capabilities                    json.RawMessage `json:"capabilities"`
		SupportedEndpointTypes          json.RawMessage `json:"supported_endpoint_types"`
		SupportedGenerationMethods      json.RawMessage `json:"supported_generation_methods"`
		SupportedGenerationMethodsCamel json.RawMessage `json:"supportedGenerationMethods"`
		Architecture                    json.RawMessage `json:"architecture"`
		Recommended                     json.RawMessage `json:"recommended"`
		IsRecommended                   json.RawMessage `json:"is_recommended"`
		Default                         json.RawMessage `json:"default"`
	}
	if err := json.Unmarshal(raw, &loose); err != nil {
		return Model{}, err
	}
	m := Model{
		ID:     loose.ID,
		Object: rawString(loose.Object),
	}
	for _, label := range []string{
		rawString(loose.Type),
		rawString(loose.ModelType),
		rawString(loose.ModelTypeCamel),
		rawString(loose.Category),
		rawString(loose.Kind),
	} {
		if label != "" {
			m.Labels = append(m.Labels, label)
		}
	}
	takeModalities(loose.Modalities, &m.Modalities, &m.OutputModalities)
	takeModalities(loose.SupportedModalities, &m.Modalities, &m.OutputModalities)
	m.Modalities = appendStrings(m.Modalities, loose.Modality)
	m.OutputModalities = appendStrings(m.OutputModalities, loose.OutputModality)
	m.OutputModalities = appendStrings(m.OutputModalities, loose.OutputModalities)
	m.OutputModalities = appendStrings(m.OutputModalities, loose.OutputModalitiesCamel)
	m.Capabilities = capabilityNames(loose.Capabilities)
	m.EndpointTypes = appendStrings(nil, loose.SupportedEndpointTypes)
	m.GenerationMethods = appendStrings(nil, loose.SupportedGenerationMethods)
	m.GenerationMethods = appendStrings(m.GenerationMethods, loose.SupportedGenerationMethodsCamel)
	decodeArchitecture(loose.Architecture, &m)
	m.Recommended = truthy(loose.Recommended) || truthy(loose.IsRecommended) || truthy(loose.Default)
	return m, nil
}

func truthy(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	switch raw[0] {
	case 't', 'T':
		return strings.EqualFold(string(raw), "true")
	case '"':
		s := strings.ToLower(rawString(raw))
		return s == "true" || s == "yes" || s == "1"
	case '1':
		return string(raw) == "1"
	default:
		return false
	}
}

func decodeArchitecture(raw json.RawMessage, m *Model) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" || raw[0] != '{' {
		return
	}
	var arch struct {
		Modality              json.RawMessage `json:"modality"`
		OutputModalities      json.RawMessage `json:"output_modalities"`
		OutputModalitiesCamel json.RawMessage `json:"outputModalities"`
	}
	if json.Unmarshal(raw, &arch) != nil {
		return
	}
	m.Modalities = appendStrings(m.Modalities, arch.Modality)
	m.OutputModalities = appendStrings(m.OutputModalities, arch.OutputModalities)
	m.OutputModalities = appendStrings(m.OutputModalities, arch.OutputModalitiesCamel)
}

func takeModalities(raw json.RawMessage, modalities *[]string, outputs *[]string) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	if raw[0] == '{' {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return
		}
		if out, ok := obj["output"]; ok {
			*outputs = appendStrings(*outputs, out)
		}
		if out, ok := obj["outputs"]; ok {
			*outputs = appendStrings(*outputs, out)
		}
		if mod, ok := obj["modality"]; ok {
			*modalities = appendStrings(*modalities, mod)
		}
		return
	}
	*modalities = appendStrings(*modalities, raw)
}

func capabilityNames(raw json.RawMessage) []string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	switch raw[0] {
	case '[':
		return appendStrings(nil, raw)
	case '"':
		return appendStrings(nil, raw)
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var names []string
	for _, k := range keys {
		switch t := obj[k].(type) {
		case bool:
			if t {
				names = append(names, k)
			}
		case string:
			s := strings.TrimSpace(t)
			if s != "" && !strings.EqualFold(s, "false") && !strings.EqualFold(s, "none") && s != "0" {
				names = append(names, k)
				names = append(names, s)
			}
		}
	}
	return names
}

func appendStrings(dst []string, raw json.RawMessage) []string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return dst
	}
	if raw[0] == '"' {
		if s := rawString(raw); s != "" {
			dst = append(dst, s)
		}
		return dst
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return dst
	}
	for _, item := range arr {
		dst = appendStrings(dst, item)
	}
	return dst
}

func rawString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" || raw[0] != '"' {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}
