package tool

import (
	"encoding/json"
	"testing"
)

// 浮点参数（秒数、比例）是合法工具参数：JSON Schema 里对应 "number"。
// 这个用例守住 schema 生成器不再对 float64 字段 panic——canvas_inspect_video
// 的 startSecond / endSecond 就是浮点，曾经让整条 Agent 运行在建工具表时崩掉。
func TestSchemaForAcceptsFloatArguments(t *testing.T) {
	type args struct {
		NodeID      string  `json:"nodeId"`
		Purpose     string  `json:"purpose"`
		FrameCount  int     `json:"frameCount,omitempty" minimum:"2" maximum:"16"`
		StartSecond float64 `json:"startSecond,omitempty" description:"观察起点（秒）" minimum:"0"`
		EndSecond   float64 `json:"endSecond,omitempty" description:"观察终点（秒）"`
	}

	raw := SchemaFor[args]()
	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema 不是合法 JSON：%v", err)
	}

	for _, name := range []string{"startSecond", "endSecond"} {
		prop, ok := schema.Properties[name]
		if !ok {
			t.Fatalf("缺少属性 %s", name)
		}
		var detail struct {
			Type    string   `json:"type"`
			Minimum *float64 `json:"minimum"`
		}
		if err := json.Unmarshal(prop, &detail); err != nil {
			t.Fatalf("属性 %s 解析失败：%v", name, err)
		}
		if detail.Type != "number" {
			t.Fatalf("浮点参数 %s 的 type 应为 number，实际 %q", name, detail.Type)
		}
	}
	// minimum:"0" 必须落到 schema 上。
	var start struct {
		Minimum *float64 `json:"minimum"`
	}
	if err := json.Unmarshal(schema.Properties["startSecond"], &start); err != nil {
		t.Fatalf("startSecond 解析失败：%v", err)
	}
	if start.Minimum == nil || *start.Minimum != 0 {
		t.Fatalf("startSecond 的 minimum 应为 0，实际 %v", start.Minimum)
	}
	// omitempty 的字段不能进 required。
	for _, name := range schema.Required {
		if name == "startSecond" || name == "endSecond" || name == "frameCount" {
			t.Fatalf("omitempty 字段 %s 不应出现在 required", name)
		}
	}
}
