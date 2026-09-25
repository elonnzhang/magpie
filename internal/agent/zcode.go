package agent

import (
	"path/filepath"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/gateway"
)

// ZCode (Zhipu's desktop app) keeps its model providers in
// ~/.zcode/v2/config.json, OpenCode's provider shape with a kind of its own:
//
//	{"provider":{"magpie":{"name":"magpie","kind":"anthropic",
//	  "options":{"apiKey":"magpie","baseURL":"http://127.0.0.1:3425"},
//	  "enabled":true,"source":"custom",
//	  "models":{"<id>":{"name":…,"limit":{"context":…},"modalities":{…}}}}}}
//
// An anthropic provider is asked at baseURL + /v1/messages. The model is
// picked per task in ZCode's own picker and kept in its window, not in a
// file, so what magpie sets is whether its models are in that picker.

func zcode(home string) *Agent {
	dir := filepath.Join(home, ".zcode")
	path := filepath.Join(dir, "v2", "config.json")
	key := "provider." + magpieID
	return &Agent{
		ID: "zcode", Name: "ZCode", Icon: "zcode", Aliases: []string{"z-code"},
		UA:  []string{"zcode"},
		Dir: dir, Path: path,
		Notice: func() string {
			if Running(`ZCode\.app/`, `(^|/)ZCode( |$)`) {
				return "ZCode reads its providers at start-up — restart ZCode to see magpie's models in its picker."
			}
			return ""
		},
		Sync: func() error { return syncJSON(path, key, zcodeProviderJSON) },
		Fields: []Field{{
			Key: "provider", Label: "provider",
			Get: func() string {
				if _, ok := edit.GetJSON(path, key); ok {
					return magpieID
				}
				return ""
			},
			Set: func(v string) error {
				if v == "" {
					return edit.DelJSON(path, key)
				}
				return edit.SetJSON(path, edit.KV{Path: key, Value: zcodeProviderJSON()})
			},
			Options: func(map[string]string) []Option {
				return []Option{{Value: magpieID, Label: "magpie", Icon: "magpie", Note: "every magpie model in ZCode's picker"}}
			},
		}},
	}
}

func zcodeProviderJSON() any {
	ms := map[string]any{}
	for _, m := range magpieModels() {
		window := m.Context
		if window == 0 {
			window = 200000
		}
		in := []string{"text"}
		if m.Images {
			in = append(in, "image")
		}
		ms[m.ID] = map[string]any{"name": m.Name, "limit": map[string]any{"context": window},
			"modalities": map[string]any{"input": in, "output": []string{"text"}}}
	}
	return map[string]any{"name": "magpie", "kind": "anthropic", "enabled": true, "source": "custom",
		"options": map[string]any{"apiKey": gateway.Token, "baseURL": gateway.URL()}, "models": ms}
}
