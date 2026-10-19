package wanchannel

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const skewFloor = 60 // 时差不到这么多秒按零算，时钟正常的厂签名不变

var skewMaxRTT = 5 * time.Second                                                      // 往返超过这么久的样本估不准，直接丢掉
var clockProbe = &http.Client{Timeout: 5 * time.Second, Transport: wanRoundTripper{}} // 拨号前取时间样本，拉不到沿用旧时差
var clockSkew atomic.Int64                                                            // WAN 钟减本机钟（秒），签名按它校正
var clockReported atomic.Int64                                                        // 上次打日志时的时差，同一偏差不反复刷

// 按 WAN 校正后的当前时间，签名用它才不会被时间窗拒绝。
func wanNow() time.Time {
	// 本机时间加上记下的时差，就是 WAN 那边的时间。
	return time.Now().Add(time.Duration(clockSkew.Load()) * time.Second)
}

// 拿到响应头就按 Date 记一次时差，正文没读完也不影响。
func sampleClock(sent time.Time, res *http.Response) {
	// 没有响应就没有 Date 可看。
	if res == nil {
		return
	}
	// 量出这次往返，太久的样本误差太大。
	rtt := time.Since(sent)
	// 往返太久就丢掉样本，沿用旧时差。
	if rtt > skewMaxRTT {
		return
	}
	// 读平台回包的 Date 头，401、404 也带着它。
	wan, err := http.ParseTime(res.Header.Get("Date"))
	// 没有或读不懂 Date 就不取样。
	if err != nil {
		return
	}
	// 取往返中点当平台写头时的本机时间。
	mid := sent.Add(rtt / 2)
	// Date 只精确到秒，补半秒再取整到秒。
	d := wan.Add(500 * time.Millisecond).Sub(mid).Round(time.Second)
	// 按秒记下时差，签名和日志都用它。
	noteSkew(int64(d / time.Second))
}

// 记下新时差；偏差明显变化时才打一条日志。
func noteSkew(raw int64) {
	// 先按原值记，小偏差再收成零。
	skew := raw
	// 不到一分钟按零算，正常厂签名和以前一样。
	if absSec(skew) < skewFloor {
		// 小偏差不校正，避免秒级抖动改签名。
		skew = 0
	}
	// 记下时差，之后签名都按它校正。
	clockSkew.Store(skew)
	// 时钟正常就不用提示。
	if skew == 0 {
		return
	}
	// 取上次报过的时差，变化不大就不再报。
	last := clockReported.Load()
	// 和上次报的差不到一分钟就不刷日志。
	if absSec(skew-last) < skewFloor {
		return
	}
	// 换上新的已报时差，并发取样时只让一个打日志。
	if !clockReported.CompareAndSwap(last, skew) {
		return
	}
	// 本机时钟偏了，告诉运维已改用 WAN 时间签名。
	slog.Warn("factory clock differs from wan, signing with wan time", "skewSeconds", skew)
}

// 拨号前不带签名问一次平台，只为从回包 Date 取时差。
func probeClock(ctx context.Context, wanHTTP string) {
	// 拼探活地址，尾斜杠去掉免得拼出双斜杠。
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(strings.TrimSpace(wanHTTP), "/")+"/healthz", nil)
	// 请求组不出来就沿用旧时差。
	if err != nil {
		return
	}
	// 记下发出时刻，往返中点要用。
	sent := time.Now()
	// 送出探活，失败就沿用旧时差。
	res, err := clockProbe.Do(req)
	// 平台不可达就不取样，拨号失败由外层退避。
	if err != nil {
		return
	}
	// 只要头不要正文，用完就关掉。
	defer res.Body.Close()
	// 按回包 Date 记下时差，后面签名用。
	sampleClock(sent, res)
}

// 秒数取绝对值，正负偏差同样处理。
func absSec(v int64) int64 {
	// 负的翻成正的，方便和门槛比较。
	if v < 0 {
		return -v
	}
	return v
}
