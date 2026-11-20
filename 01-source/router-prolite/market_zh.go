package main

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// 离线翻译缓存随程序一起发布，键为“包名@版本”，未命中的条目由前端回退英文并标注“待翻译”。
//
//go:embed frontend/market-zh.json
var marketZhJSON []byte

var (
	marketZhOnce sync.Once
	marketZhMap  map[string]struct {
		D string `json:"d"`
	}
)

func marketZhText(name, version string) string {
	marketZhOnce.Do(func() {
		marketZhMap = map[string]struct {
			D string `json:"d"`
		}{}
		_ = json.Unmarshal(marketZhJSON, &marketZhMap)
	})
	if v, ok := marketZhMap[name+"@"+version]; ok {
		return v.D
	}
	return ""
}
