package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	agenttool "yingce/backend/internal/agent/tool"
	"yingce/backend/internal/model"
)

func TestCloudAgentVideoInspectRequiresReadyLocalVideo(t *testing.T) {
	s, db, _, _ := creationTestService(t)
	canvas := &model.CanvasProject{ID: "video-canvas", UserID: "user", PayloadJSON: `{"nodes":[{"id":"vid-1","type":"video","title":"原片","metadata":{"storageKey":"resource:video-missing"}}],"connections":[]}`}
	if err := db.Create(canvas).Error; err != nil {
		t.Fatal(err)
	}
	state := &cloudAgentRuntime{RuntimeRunID: "video-run", Request: CloudAgentRequest{CanvasID: canvas.ID}, TransientReferences: map[string]cloudAgentTransientReference{}}
	call := cloudAgentCall{ID: "call-video-1"}
	call.Function.Name = agenttool.CanvasInspectVideo
	call.Function.Arguments = `{"nodeId":"vid-1","purpose":"识别剧情用于复刻分镜"}`

	if _, err := cloudAgentReadTool(s.repo, "user", state, call, s); err == nil {
		t.Fatal("missing video resource should fail")
	} else if !strings.Contains(cloudAgentSafeToolError(err), "视频资源不存在") {
		t.Fatalf("unexpected error for missing resource: %v", err)
	}
}

func TestCloudAgentVideoInspectRejectsImageNodeAndBudget(t *testing.T) {
	s, db, _, _ := creationTestService(t)
	canvas := &model.CanvasProject{ID: "video-canvas-2", UserID: "user", PayloadJSON: `{"nodes":[{"id":"img-1","type":"image","title":"图","metadata":{"storageKey":"resource:img-x"}}],"connections":[]}`}
	if err := db.Create(canvas).Error; err != nil {
		t.Fatal(err)
	}
	state := &cloudAgentRuntime{RuntimeRunID: "video-run-2", Request: CloudAgentRequest{CanvasID: canvas.ID}, TransientReferences: map[string]cloudAgentTransientReference{}}

	call := cloudAgentCall{ID: "call-video-2"}
	call.Function.Name = agenttool.CanvasInspectVideo
	call.Function.Arguments = `{"nodeId":"img-1","purpose":"看图"}`
	if _, err := cloudAgentReadTool(s.repo, "user", state, call, s); err == nil {
		t.Fatal("image node must be rejected")
	} else if !strings.Contains(cloudAgentSafeToolError(err), "不是视频节点") {
		t.Fatalf("unexpected error for image node: %v", err)
	}

	call2 := cloudAgentCall{ID: "call-video-3"}
	call2.Function.Name = agenttool.CanvasInspectVideo
	call2.Function.Arguments = `{"nodeId":"img-1","purpose":"看图","frameCount":99}`
	if _, err := cloudAgentReadTool(s.repo, "user", state, call2, s); err == nil {
		t.Fatal("frameCount=99 must be rejected")
	}

	// 预算：VideoFramesDelivered 已满时直接拒绝。
	state.VideoFramesDelivered = canvasAgentVideoInspectMaxFramesPerRun
	call3 := cloudAgentCall{ID: "call-video-4"}
	call3.Function.Name = agenttool.CanvasInspectVideo
	call3.Function.Arguments = `{"nodeId":"img-1","purpose":"看图"}`
	if _, err := cloudAgentReadTool(s.repo, "user", state, call3, s); err == nil {
		t.Fatal("exhausted frame budget must be rejected")
	} else if !strings.Contains(cloudAgentSafeToolError(err), "帧预算") {
		t.Fatalf("unexpected budget error: %v", err)
	}
}

func TestCloudAgentVideoInspectExtractsFramesWhenFFmpegAvailable(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Log("无 ffmpeg，跳过真实抽帧断言")
		return
	}
	s, db, _, _ := creationTestService(t)
	// 用 ffmpeg 生成 2 秒测试视频（testsrc + 正弦音），落进资源存储约定路径。
	objectKey := "agent-test/sample.mp4"
	src := filepath.Join(s.dataDir, "resources", filepath.FromSlash(objectKey))
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=2:size=320x180:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=1000:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-y", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("生成测试视频失败：%v\n%s", err, out)
	}
	info, err := probeMediaFile(context.Background(), src)
	if err != nil {
		t.Fatalf("探测测试视频失败：%v", err)
	}
	if info.DurationMs < 1500 {
		t.Fatalf("测试视频时长异常：%dms", info.DurationMs)
	}
	res := &model.Resource{ID: "video-local", UserID: "user", Kind: "video", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: objectKey, MimeType: "video/mp4", Size: 1, DurationMs: info.DurationMs}
	if err := db.Create(res).Error; err != nil {
		t.Fatal(err)
	}
	canvas := &model.CanvasProject{ID: "video-canvas-3", UserID: "user", PayloadJSON: `{"nodes":[{"id":"vid-1","type":"video","title":"原片","metadata":{"storageKey":"resource:video-local"}}],"connections":[]}`}
	if err := db.Create(canvas).Error; err != nil {
		t.Fatal(err)
	}
	state := &cloudAgentRuntime{RuntimeRunID: "video-run-3", Request: CloudAgentRequest{CanvasID: canvas.ID}, TransientReferences: map[string]cloudAgentTransientReference{}}
	call := cloudAgentCall{ID: "call-video-5"}
	call.Function.Name = agenttool.CanvasInspectVideo
	call.Function.Arguments = `{"nodeId":"vid-1","purpose":"识别剧情用于复刻分镜","frameCount":4}`
	result, err := cloudAgentReadTool(s.repo, "user", state, call, s)
	if err != nil {
		t.Fatal(err)
	}
	batch, ok := result.(cloudAgentImageInspectionBatch)
	if !ok {
		t.Fatalf("result type = %T, want cloudAgentImageInspectionBatch", result)
	}
	if len(batch.Inspections) != 4 {
		t.Fatalf("inspections = %d, want 4", len(batch.Inspections))
	}
	for i, inspection := range batch.Inspections {
		if !strings.HasPrefix(inspection.ImageURL, "resource:") {
			t.Fatalf("frame %d image url = %q", i, inspection.ImageURL)
		}
		if inspection.CacheKey != "" {
			t.Fatalf("frame %d should not use canvas-revision cache key", i)
		}
	}
	if state.VideoFramesDelivered != 4 {
		t.Fatalf("VideoFramesDelivered = %d, want 4", state.VideoFramesDelivered)
	}
	if len(state.TransientReferences) != 4 {
		t.Fatalf("transient references = %d, want 4", len(state.TransientReferences))
	}
	receipt, _ := json.Marshal(batch.Receipt)
	for _, key := range []string{"durationS", "frames", "frameCount"} {
		if !strings.Contains(string(receipt), key) {
			t.Fatalf("receipt missing %s: %s", key, receipt)
		}
	}
}
