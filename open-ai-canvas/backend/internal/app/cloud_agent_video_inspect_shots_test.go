package app

import (
	"testing"
)

// 镜头切分是本次改动的核心：切点必须决定帧落点，而不是时间等分。
func TestPlanFramePointsUsesShotStartsWhenShotsFitBudget(t *testing.T) {
	// 模拟一条 20 秒片子的 4 个镜头：5s / 2.5s / 7.5s / 5s。
	shots := []videoShotSpan{
		{Index: 1, StartS: 0, EndS: 5, DurationS: 5},
		{Index: 2, StartS: 5, EndS: 7.5, DurationS: 2.5},
		{Index: 3, StartS: 7.5, EndS: 15, DurationS: 7.5},
		{Index: 4, StartS: 15, EndS: 20, DurationS: 5},
	}
	points := planFramePoints(shots, 20, 8)
	if len(points) != 8 {
		t.Fatalf("期望 8 帧，得到 %d", len(points))
	}
	// 每镜起点后 10% 偏移：镜头 1 起点 0 → 0.5；镜头 2 起点 5 → 5.25。
	// 每镜至少一帧必须落在自己的镜头区间内，这是「不跨镜」的底线。
	perShot := make([]int, len(shots))
	for _, at := range points {
		for i, shot := range shots {
			if at >= shot.StartS && at < shot.EndS {
				perShot[i]++
				break
			}
		}
	}
	for i, count := range perShot {
		if count == 0 {
			t.Fatalf("镜头 %d 没有分到帧，帧落点跨过了镜头边界", i+1)
		}
	}
	// 剩余 4 帧补给最长的镜头：镜头 3（7.5s）与镜头 1、4（各 5s）。
	if perShot[2] < 2 {
		t.Fatalf("最长镜头应补中点帧，实际只分到 %d 帧", perShot[2])
	}
}

func TestPlanFramePointsSpreadShotStartsWhenShotsExceedBudget(t *testing.T) {
	// 25 镜、只给 8 帧：无法逐镜覆盖，但落点必须是真实镜头起点，不能是等分时刻。
	shots := make([]videoShotSpan, 25)
	for i := range shots {
		shots[i] = videoShotSpan{Index: i + 1, StartS: float64(i) * 4, EndS: float64(i+1) * 4, DurationS: 4}
	}
	points := planFramePoints(shots, 100, 8)
	if len(points) != 8 {
		t.Fatalf("期望 8 帧，得到 %d", len(points))
	}
	starts := make(map[float64]bool, len(shots))
	for _, shot := range shots {
		starts[shot.StartS] = true
	}
	for _, at := range points {
		if !starts[at] {
			t.Fatalf("帧落点 %.2fs 不是任何镜头起点，说明退回了等分抽帧", at)
		}
	}
	// 覆盖应尽量均匀铺开：首尾都要有。
	if points[0] != 0 {
		t.Fatalf("首帧应从第一个镜头起点开始，实际 %.2f", points[0])
	}
	if points[len(points)-1] < 80 {
		t.Fatalf("末帧应覆盖到片子后段，实际 %.2f", points[len(points)-1])
	}
}

func TestPlanFramePointsFallsBackToEvenSplitWithoutShots(t *testing.T) {
	points := planFramePoints(nil, 60, 6)
	if len(points) != 6 {
		t.Fatalf("期望 6 帧，得到 %d", len(points))
	}
	for i, at := range points {
		want := float64(i) * 60 / 6
		if at != want {
			t.Fatalf("第 %d 帧应为 %.2f，实际 %.2f", i, want, at)
		}
	}
}

// 时间窗口是长视频（10 分钟切出上百镜）的唯一出路：单轮 16 帧必须集中在
// 用户指定的那一段，窗口外的镜头不参与分配。
func TestPlanFramePointsInWindowOnlyUsesShotsInsideWindow(t *testing.T) {
	// 30 个 10 秒镜头，共 300 秒；只看 100-140 秒（第 11-14 镜）。
	shots := make([]videoShotSpan, 30)
	for i := range shots {
		shots[i] = videoShotSpan{Index: i + 1, StartS: float64(i) * 10, EndS: float64(i+1) * 10, DurationS: 10}
	}
	points := planFramePointsInWindow(shots, 100, 140, 8)
	if len(points) != 8 {
		t.Fatalf("期望 8 帧，得到 %d", len(points))
	}
	for _, at := range points {
		if at < 100 || at > 140 {
			t.Fatalf("帧落点 %.2fs 跑到窗口 100-140s 之外", at)
		}
	}
	// 窗口内 4 镜给 8 帧：每镜起点后 0.5s（10s*10% 超过 0.5 上限）各一帧，
	// 余 4 帧按镜头时长降序补中点（时长相同则按顺序补前 4 个，恰好每镜第二帧）。
	inWindow := map[int]int{}
	for _, at := range points {
		idx := int(at-100) / 10
		inWindow[idx]++
	}
	if len(inWindow) != 4 {
		t.Fatalf("窗口内 4 个镜头应全部拿到帧，实际覆盖 %d 个: %v", len(inWindow), inWindow)
	}
}

func TestPlanFramePointsInWindowFallsBackToEvenSplitWhenNoCutInside(t *testing.T) {
	// 整片一个镜头（一镜到底），指定 20-60 秒：窗口内无切点，应在窗口内等分。
	shots := []videoShotSpan{{Index: 1, StartS: 0, EndS: 200, DurationS: 200}}
	points := planFramePointsInWindow(shots, 20, 60, 4)
	if len(points) != 4 {
		t.Fatalf("期望 4 帧，得到 %d", len(points))
	}
	for i, at := range points {
		want := 20 + float64(i)*40/4
		if at != want {
			t.Fatalf("第 %d 帧应为 %.2f，实际 %.2f", i, want, at)
		}
	}
}

func TestPlanFramePointsInWindowRejectsEmptyWindow(t *testing.T) {
	shots := []videoShotSpan{{Index: 1, StartS: 0, EndS: 10, DurationS: 10}}
	if points := planFramePointsInWindow(shots, 10, 10, 8); len(points) != 0 {
		t.Fatalf("零长度窗口应返回空，得到 %v", points)
	}
	if points := planFramePointsInWindow(shots, 5, 1, 8); len(points) != 0 {
		t.Fatalf("反向窗口应返回空，得到 %v", points)
	}
}

// 窗口覆盖率是「能不能进入下一步」的判据：回执要靠它告诉模型还有多少镜没画面。
func TestShotsInWindowCountsOnlyIntersectingShots(t *testing.T) {
	shots := make([]videoShotSpan, 10)
	for i := range shots {
		shots[i] = videoShotSpan{Index: i + 1, StartS: float64(i) * 10, EndS: float64(i+1) * 10, DurationS: 10}
	}
	// 20-45 秒：完整覆盖第 3、4 镜，与第 5 镜（40-50）有交集 → 共 3 镜。
	if got := shotsInWindow(shots, 20, 45); got != 3 {
		t.Fatalf("期望 3 个镜头落在窗口内，实际 %d", got)
	}
	// 边界零长度接触不算覆盖：10-20 秒只完整覆盖第 2 镜（第 1、3 镜端点相切）。
	if got := shotsInWindow(shots, 10, 20); got != 1 {
		t.Fatalf("边界相切不应计入，期望 1，实际 %d", got)
	}
	if got := shotsInWindow(shots, 0, 100); got != 10 {
		t.Fatalf("全片窗口应覆盖全部 10 镜，实际 %d", got)
	}
}

// 分段计划是「166 镜只有 16 帧」场景的导航：每轮边界必须落在真实镜头边界上，
// 且相邻两轮不重不漏地覆盖全部镜头。
func TestSuggestInspectWindowsTilesAllShots(t *testing.T) {
	// 40 个不等长镜头，每轮 16 帧 → 应为 3 轮（16 + 16 + 8）。
	shots := make([]videoShotSpan, 40)
	at := 0.0
	for i := range shots {
		d := 2.0 + float64(i%5)
		shots[i] = videoShotSpan{Index: i + 1, StartS: at, EndS: at + d, DurationS: d}
		at += d
	}
	plan := suggestInspectWindows(shots, 16)
	if len(plan) != 3 {
		t.Fatalf("40 镜每轮 16 帧应为 3 轮，实际 %d", len(plan))
	}
	total := 0
	for i, w := range plan {
		n := w["shotCount"].(int)
		total += n
		if n > 16 {
			t.Fatalf("第 %d 轮含 %d 镜，超过每轮帧预算", i+1, n)
		}
		// 区间起点必须是该轮第一个镜头的起点，终点必须是最后一个镜头的终点。
		from := w["fromShot"].(int)
		to := w["toShot"].(int)
		if w["startSecond"].(float64) != shots[from-1].StartS {
			t.Fatalf("第 %d 轮起点 %.2f 不是镜头 %d 的起点 %.2f",
				i+1, w["startSecond"], from, shots[from-1].StartS)
		}
		if w["endSecond"].(float64) != shots[to-1].EndS {
			t.Fatalf("第 %d 轮终点 %.2f 不是镜头 %d 的终点 %.2f",
				i+1, w["endSecond"], to, shots[to-1].EndS)
		}
	}
	if total != len(shots) {
		t.Fatalf("各轮镜头数合计 %d，应等于 %d（不重不漏）", total, len(shots))
	}
}

func TestSuggestInspectWindowsNeedsOneRoundWhenShotsFit(t *testing.T) {
	shots := make([]videoShotSpan, 10)
	for i := range shots {
		shots[i] = videoShotSpan{Index: i + 1, StartS: float64(i), EndS: float64(i + 1), DurationS: 1}
	}
	plan := suggestInspectWindows(shots, 16)
	if len(plan) != 1 {
		t.Fatalf("10 镜在 16 帧预算内应为 1 轮，实际 %d", len(plan))
	}
}

func TestDetectVideoShotsParsesAndDedupesCuts(t *testing.T) {
	// 直接验证切点解析与去重：构造一段 showinfo 风格输出。
	// detectVideoShots 需要真实 ffmpeg，这里只覆盖纯文本解析路径。
	cuts := parseSceneCuts("pts_time:0.25\npts_time:0.5\npts_time:5.25\npts_time:9999\n", 113.45)
	if len(cuts) != 2 {
		t.Fatalf("期望 2 个切点（去重 + 越界剔除），得到 %d: %v", len(cuts), cuts)
	}
	if cuts[0] != 0.25 || cuts[1] != 5.25 {
		t.Fatalf("切点解析错误: %v", cuts)
	}
}
