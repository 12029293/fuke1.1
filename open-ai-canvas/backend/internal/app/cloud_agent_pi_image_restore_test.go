package app

import (
	"testing"
)

// 复刻 ag3920 run 的断链场景：抽帧回执进入运行时投影（纯文本），图片 user 消息
// 只存在于服务端 canonical，且每次恢复多出的 system 条目让两者长度打平、投影获胜。
// 修复后投影必须把未消费的看图消息补回到锚点 tool 回执之后。
func TestCloudAgentPiCanonicalRestoreImageMessages(t *testing.T) {
	inspectReceipt := `{"durationS":113.45,"frameCount":16,"frames":[{"index":1,"timestampS":0,"resourceId":"1561d66baef5fddfeee32c6eff67f298"}],"note":"已按时间均匀抽帧"}`
	server := []map[string]any{
		{"role": "system", "content": "contract"},
		{"role": "user", "content": "复刻这个视频"},
		{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "call_video", "function": map[string]any{"name": "canvas_inspect_video", "arguments": "{}"}},
			map[string]any{"id": "call_state", "function": map[string]any{"name": "canvas_get_state", "arguments": "{}"}},
		}},
		{"role": "tool", "tool_call_id": "call_video", "content": inspectReceipt},
		{"role": "tool", "tool_call_id": "call_state", "content": `{"canvasRevision":7}`},
		// flush 追加的看图消息：文字回执 + resource 图片占位。
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": cloudAgentImageCaptionHead + "\n" + inspectReceipt},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "resource:frame-01"}},
			map[string]any{"type": "text", "text": cloudAgentImageCaption(1) + "\n" + `{"frame":2}`},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "resource:frame-02"}},
		}},
		// 已被消费的看图消息：只剩文字注记，不该被补回。
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": cloudAgentImageCaptionHead + "\n旧回执"},
			map[string]any{"type": "text", "text": cloudAgentImageEvictionNote(nil, nil)},
		}},
	}
	// 运行时投影：多一条恢复用的 system 条目，长度与服务端历史一致但没有任何图片。
	runtime := []map[string]any{
		{"role": "system", "content": "resume note"},
		{"role": "user", "content": []any{map[string]any{"type": "text", "text": "复刻这个视频"}}},
		{"role": "assistant", "content": []any{
			map[string]any{"type": "toolCall", "id": "call_video", "name": "canvas_inspect_video", "arguments": map[string]any{}},
			map[string]any{"type": "toolCall", "id": "call_state", "name": "canvas_get_state", "arguments": map[string]any{}},
		}},
		{"role": "toolResult", "toolCallId": "call_video", "content": []any{map[string]any{"type": "text", "text": inspectReceipt}}},
		{"role": "toolResult", "toolCallId": "call_state", "content": []any{map[string]any{"type": "text", "text": `{"canvasRevision":7}`}}},
	}

	out := cloudAgentPiCanonicalRestoreImageMessages(runtime, server)
	if len(out) != len(runtime)+1 {
		t.Fatalf("restored length = %d, want %d", len(out), len(runtime)+1)
	}
	index := -1
	for i, message := range out {
		if stringField(message, "role") == "user" && cloudAgentIsInspectedImageMessage(message) {
			index = i
		}
	}
	if index < 0 {
		t.Fatalf("inspected image message not restored")
	}
	// 必须正好插在最后一条 tool 回执（call_state）之后。
	if stringField(out[index-1], "role") != "toolResult" || stringField(out[index-1], "toolCallId") != "call_state" {
		t.Fatalf("image message anchored at %d, previous message = %+v", index, out[index-1])
	}
	// 图片 part 必须都在，模型请求才能带画面。
	if !cloudAgentMessageHasImageURLPart(out[index]) {
		t.Fatalf("restored message has no image_url parts")
	}
	// 消费过的历史消息不能再次出现：整条恢复结果里只允许一条看图消息。
	count := 0
	for _, message := range out {
		if cloudAgentIsInspectedImageMessage(message) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("restored %d inspected image messages, want 1", count)
	}
}

// 锚点回执不在投影里（会话被截断等）时宁可丢图也不能破坏配对。
func TestCloudAgentPiCanonicalRestoreImageMessagesAnchorMissing(t *testing.T) {
	server := []map[string]any{
		{"role": "tool", "tool_call_id": "call_gone", "content": "{}"},
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": cloudAgentImageCaptionHead + "\n回执"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "resource:frame-01"}},
		}},
	}
	runtime := []map[string]any{{"role": "user", "content": "继续"}}
	out := cloudAgentPiCanonicalRestoreImageMessages(runtime, server)
	if len(out) != 1 {
		t.Fatalf("length = %d, want unchanged 1", len(out))
	}
	if cloudAgentIsInspectedImageMessage(out[0]) {
		t.Fatalf("image message should not be restored without its anchor receipt")
	}
}

// 恢复后的投影要能通过 canonical 协议校验，直接进入模型请求。
// 生产路径传入的是 canonicalFromRuntimeMessages 的输出（canonical 格式），这里保持同形。
func TestCloudAgentPiCanonicalRestoreImageMessagesValidHistory(t *testing.T) {
	server := []map[string]any{
		{"role": "user", "content": "复刻这个视频"},
		{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "call_video", "function": map[string]any{"name": "canvas_inspect_video", "arguments": "{}"}},
		}},
		{"role": "tool", "tool_call_id": "call_video", "content": `{"frameCount":16}`},
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": cloudAgentImageCaptionHead + "\n" + `{"frameCount":16}`},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "resource:frame-01"}},
		}},
	}
	runtime := []map[string]any{
		{"role": "user", "content": "复刻这个视频"},
		{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "call_video", "function": map[string]any{"name": "canvas_inspect_video", "arguments": "{}"}},
		}},
		{"role": "tool", "tool_call_id": "call_video", "content": `{"frameCount":16}`},
	}
	out := cloudAgentPiCanonicalRestoreImageMessages(runtime, server)
	canonical := canonicalAgentRequest{Messages: out}
	if err := validateCanonicalAgentHistory(&canonical, false); err != nil {
		t.Fatalf("validateCanonicalAgentHistory: %v", err)
	}
	if len(out) != 4 {
		t.Fatalf("length = %d, want 4", len(out))
	}
	if stringField(out[3], "role") != "user" || !cloudAgentMessageHasImageURLPart(out[3]) {
		t.Fatalf("message after tool receipt is not the restored image message: %+v", out[3])
	}
}
