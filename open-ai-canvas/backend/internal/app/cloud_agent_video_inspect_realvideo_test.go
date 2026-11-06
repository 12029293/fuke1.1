package app

import (
	"os"
	"os/exec"
	"testing"
)

// 这条测试用真实视频验证「分段看片」的核心承诺：按 suggestedWindows 逐轮看片时，
// 每一轮窗口内的每个镜头都能分到至少一帧。没有 ffmpeg 或测试视频时跳过。
//
// 测试视频路径可用环境变量 YINGCE_VIDEO_PROBE 覆盖，默认取本机实测样本
// （600 秒、166 镜的长片）。
func TestRealVideoWindowPlanCoversEveryShotPerRound(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("未安装 ffmpeg，跳过真实视频验证")
	}
	video := os.Getenv("YINGCE_VIDEO_PROBE")
	if video == "" {
		video = `D:\影策\data\resources\users\4c87a9e7963fe2a42a9db43672f3724e\video\2026\11\04\69fcb8c872c0adb900dc779f7e5ef390.mp4`
	}
	if _, statErr := os.Stat(video); statErr != nil {
		t.Skipf("测试视频不存在，跳过：%s", video)
	}

	info, probeErr := probeMediaFile(t.Context(), video)
	if probeErr != nil || info.DurationMs <= 0 {
		t.Fatalf("探测视频时长失败：%v", probeErr)
	}
	durationS := float64(info.DurationMs) / 1000

	shots := detectVideoShots(ffmpeg, video, durationS)
	if len(shots) == 0 {
		t.Fatalf("场景检测没有切出任何镜头（%.2fs）", durationS)
	}
	t.Logf("视频 %.2fs → %d 镜", durationS, len(shots))

	const framesPerRound = 16
	plan := suggestInspectWindows(shots, framesPerRound)
	if len(plan) == 0 {
		t.Fatalf("分段计划为空")
	}
	t.Logf("逐镜全覆盖需要 %d 轮", len(plan))

	// 逐轮验证：该轮窗口内的每个镜头都必须分到帧。
	for i, w := range plan {
		wStart := w["startSecond"].(float64)
		wEnd := w["endSecond"].(float64)
		points := planFramePointsInWindow(shots, wStart, wEnd, framesPerRound)
		if len(points) == 0 {
			t.Fatalf("第 %d 轮（%.2f-%.2f）没有分配任何帧", i+1, wStart, wEnd)
		}
		// 窗口内的镜头（按本轮的 fromShot/toShot 判定）应全部被帧覆盖。
		from := w["fromShot"].(int)
		to := w["toShot"].(int)
		hit := make(map[int]bool, to-from+1)
		for _, at := range points {
			for idx := from - 1; idx <= to-1; idx++ {
				shot := shots[idx]
				if at >= shot.StartS && at < shot.EndS {
					hit[idx] = true
				}
			}
		}
		if len(hit) != to-from+1 {
			missing := make([]int, 0, 4)
			for idx := from - 1; idx <= to-1; idx++ {
				if !hit[idx] {
					missing = append(missing, shots[idx].Index)
				}
			}
			t.Fatalf("第 %d 轮（%.2f-%.2f）有 %d 个镜头没分到帧：%v",
				i+1, wStart, wEnd, len(missing), missing)
		}
		// 帧落点必须在窗口内，不许越界。
		for _, at := range points {
			if at < wStart || at > wEnd {
				t.Fatalf("第 %d 轮帧落点 %.2fs 越出窗口 %.2f-%.2f", i+1, at, wStart, wEnd)
			}
		}
	}
}
