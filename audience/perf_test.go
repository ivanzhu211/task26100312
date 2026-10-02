package audience

import (
	"fmt"
	"math/rand"
	"runtime"
	"testing"
	"time"
)

// countPositions 统计表达式中标签与运算符的出现位置总数。
func countPositions(expr *Expr) int {
	n := 1
	for _, c := range expr.Children {
		n += countPositions(c)
	}
	return n
}

// maxDepth 统计表达式嵌套层数。
func maxDepth(expr *Expr) int {
	d := 0
	for _, c := range expr.Children {
		if cd := maxDepth(c); cd > d {
			d = cd
		}
	}
	return d + 1
}

// buildScaleAds 生成 100000 条广告：绝大多数为 AND(20 组 OR 二选一)，
// 少量为含 NOT 的恒真表达式，标签取自 1..1,000,000。
func buildScaleAds(r *rand.Rand) ([]Ad, int) {
	const (
		numAds   = 100000
		maxTag   = 1000000
		pairs    = 20
		notEvery = 2000 // 每 2000 条放 1 条恒真 NOT 表达式
	)
	ads := make([]Ad, numAds)
	total := 0
	for i := 0; i < numAds; i++ {
		var expr *Expr
		if i%notEvery == 0 {
			expr = Not(Tag(r.Int63n(maxTag) + 1))
		} else {
			children := make([]*Expr, pairs)
			for j := range children {
				children[j] = Or(Tag(r.Int63n(maxTag)+1), Tag(r.Int63n(maxTag)+1))
			}
			expr = And(children...)
		}
		total += countPositions(expr)
		if d := maxDepth(expr); d > 32 {
			panic(fmt.Sprintf("嵌套超限: %d", d))
		}
		ads[i] = Ad{ID: int64(i + 1), Expr: expr}
	}
	return ads, total
}

func heapAlloc() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestScaleAndPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("规模测试，-short 时跳过")
	}
	r := rand.New(rand.NewSource(42))
	ads, total := buildScaleAds(r)
	t.Logf("广告数=%d 总出现位置=%d", len(ads), total)

	base := heapAlloc()
	start := time.Now()
	e := NewEngine()
	if err := e.Load(ads); err != nil {
		t.Fatalf("Load: %v", err)
	}
	loadDur := time.Since(start)
	loadHeap := heapAlloc()
	t.Logf("载入耗时=%v 载入后堆内存=%.1fMiB(增量 %.1fMiB)",
		loadDur, float64(loadHeap)/1048576, float64(loadHeap-base)/1048576)
	if loadDur > 30*time.Second {
		t.Fatalf("载入超时: %v > 30s", loadDur)
	}
	if loadHeap > 1<<30 {
		t.Fatalf("堆内存超限: %.1fMiB > 1024MiB", float64(loadHeap)/1048576)
	}

	// 构造 1000 个请求，每个 50 个标签
	const numReq = 1000
	requests := make([][]int64, numReq)
	for i := range requests {
		tags := make([]int64, 50)
		for j := range tags {
			tags[j] = r.Int63n(1000000) + 1
		}
		requests[i] = tags
	}

	var totalDur, maxDur time.Duration
	totalHits := 0
	for _, tags := range requests {
		s := time.Now()
		got := e.Match(tags)
		d := time.Since(s)
		totalDur += d
		if d > maxDur {
			maxDur = d
		}
		totalHits += len(got)
	}
	peakHeap := heapAlloc()
	t.Logf("1000 次匹配总耗时=%v 平均=%v 单次最大=%v 平均命中=%.1f",
		totalDur, totalDur/numReq, maxDur, float64(totalHits)/numReq)
	t.Logf("匹配后堆内存=%.1fMiB", float64(peakHeap)/1048576)
	if totalDur > 10*time.Second {
		t.Fatalf("1000 次匹配总耗时超限: %v > 10s", totalDur)
	}
	if maxDur > 100*time.Millisecond {
		t.Fatalf("单次匹配超限: %v > 100ms", maxDur)
	}

	// 抽样与逐条直接求值对拍，确认加速路径不漏否定条件
	for _, tags := range requests[:5] {
		want := refMatch(ads, tags)
		got := e.Match(tags)
		if len(got) != len(want) {
			t.Fatalf("命中数量不一致: got=%d want=%d", len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("命中集合不一致: got=%v want=%v", got, want)
			}
		}
	}
}
