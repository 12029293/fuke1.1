package app

// 把服务端历史里的看图 user 消息按锚点补回运行时投影（见
// cloudAgentPiCanonicalRestoreImageMessages 的背景说明）。
//
// 锚点定位：服务端 flush 顺序是 assistant(tool_calls×N) → tool 结果×N →
// user(看图消息)（cloudAgentFlushPendingImages），所以每条看图消息前面最近的一条
// tool 消息就是本批最后一次调用的回执，其 call id 即锚点。这是对存量历史的结构推断，
// 不依赖回执 JSON 里的任何字段；图片被消费或裁剪后消息只剩文字注记，天然不参与。
func cloudAgentServerImageMessageAnchors(existing []map[string]any) map[string][]map[string]any {
	anchors := map[string][]map[string]any{}
	lastToolCallID := ""
	for _, message := range existing {
		switch stringField(message, "role") {
		case "tool":
			id := ""
			for _, key := range []string{"tool_call_id", "toolCallId", "call_id"} {
				if value := stringField(message, key); value != "" {
					id = value
					break
				}
			}
			if id != "" {
				lastToolCallID = id
			}
		case "user":
			if lastToolCallID == "" {
				continue
			}
			if !cloudAgentIsInspectedImageMessage(message) || !cloudAgentMessageHasImageURLPart(message) {
				continue
			}
			anchors[lastToolCallID] = append(anchors[lastToolCallID], message)
		}
	}
	return anchors
}
