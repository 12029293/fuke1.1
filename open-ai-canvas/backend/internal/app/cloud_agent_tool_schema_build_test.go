package app

import (
	"encoding/json"
	"testing"
)

// 所有工具的 schema 必须在启动时就构建成功。这个用例守住「加工具参数导致
// 整条 Agent 运行建工具表时崩掉」这类问题——canvas_inspect_video 的
// startSecond / endSecond 曾因 schema 生成器不支持 float64 而 panic，
// 用户表现为「点复刻什么都不出来」，只在全量测试里才暴露。
func TestCloudAgentAllToolSchemasBuild(t *testing.T) {
	tools := cloudAgentAllToolSchemas()
	if len(tools) == 0 {
		t.Fatal("工具 schema 列表为空")
	}
	seen := make(map[string]bool, len(tools))
	for _, entry := range tools {
		fn, ok := entry["function"].(map[string]any)
		if !ok {
			t.Fatalf("工具条目缺少 function 对象：%v", entry)
		}
		name, _ := fn["name"].(string)
		if name == "" {
			t.Fatalf("工具条目缺少 name：%v", entry)
		}
		if seen[name] {
			t.Fatalf("工具 %s 的 schema 重复注册", name)
		}
		seen[name] = true
		// schema 必须能序列化成合法 JSON（否则发请求前就会失败）。
		if _, err := json.Marshal(fn["parameters"]); err != nil {
			t.Fatalf("工具 %s 的 parameters 无法序列化：%v", name, err)
		}
	}
	if !seen["canvas_inspect_video"] {
		t.Fatalf("canvas_inspect_video 不在工具表里，实际 %d 个：%v", len(seen), seen)
	}
}

// canvas_inspect_video 的时间窗口参数必须真的出现在 schema 里，
// 否则模型看不到 startSecond / endSecond，分段看片根本用不上。
func TestCanvasInspectVideoSchemaExposesTimeWindow(t *testing.T) {
	var params map[string]any
	for _, entry := range cloudAgentAllToolSchemas() {
		fn, _ := entry["function"].(map[string]any)
		if fn == nil || fn["name"] != "canvas_inspect_video" {
			continue
		}
		params, _ = fn["parameters"].(map[string]any)
	}
	if params == nil {
		t.Fatal("没找到 canvas_inspect_video 的 parameters")
	}
	properties, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("canvas_inspect_video 缺少 properties：%v", params)
	}
	for _, key := range []string{"startSecond", "endSecond"} {
		prop, ok := properties[key].(map[string]any)
		if !ok {
			t.Fatalf("缺少时间窗口参数 %s", key)
		}
		if prop["type"] != "number" {
			t.Fatalf("%s 的 type 应为 number，实际 %v", key, prop["type"])
		}
	}
}
