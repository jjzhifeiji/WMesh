package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/blob"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/disk"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

const (
	SoftwareFactoryService = store.SoftwareFactoryService // 厂端服务包
	SoftwareClientAPK      = store.SoftwareClientAPK      // 客户端 APK
	SoftwareWANService     = "wan_service"                // 云端包，本厂不得收
)

// SoftwareMeta 是一份软件包的元数据，不含字节。
type SoftwareMeta struct {
	Kind        string `json:"kind"`        // factory_service / client_apk
	Version     int64  `json:"version"`     // 单调整数
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // SHA-256
}

// SoftwareOffer 是带正文的一份软件包。
type SoftwareOffer struct {
	Kind        string `json:"kind"`        // factory_service / client_apk
	Version     int64  `json:"version"`     // 单调整数
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // SHA-256
	Body        []byte `json:"body"`        // 包字节
}

// SoftwareSource 从已认领厂会话背后的 WAN 拉最高包。
type SoftwareSource interface {
	// 问云端该种类当前最高版本，不含包字节。
	Latest(ctx context.Context, kind string) (SoftwareMeta, error)
	// 按版本把包字节拉回；want 是问最高版拿到的摘要，可空，非空时必须对上。
	Pull(ctx context.Context, kind string, version int64, want []byte) ([]byte, error)
	// 本厂已收下这一版，源侧可清掉它的暂存；只动本地，不联网。
	Received(kind string, version int64, sum []byte)
}

// ApplyOutcome 决定确认后是否立刻记已装；测试夹具用，生产默认只落盘。
type ApplyOutcome int

const (
	ApplyDefer ApplyOutcome = iota // 只写待切换，不记已装
	ApplyOK                        // 夹具宣称落地成功
	ApplyFail                      // 夹具宣称落地失败
)

// Updates 管本厂自拉软件包、超管确认厂服务、本机确认客户端。
type Updates struct {
	*kernel // 登录、审计和本厂库
}

// softwareTarget 审计对象：种类和版本。
func softwareTarget(kind string, version int64) string {
	// 把种类和版本拼成审计对象。
	return kind + " v" + strconv.FormatInt(version, 10)
}

// factoryPullKind 本厂只收厂服务包和客户端包。
func factoryPullKind(kind string) bool {
	return kind == SoftwareFactoryService || kind == SoftwareClientAPK
}

// SetApplyOutcome 测试注入落地结果；生产保持默认只落盘。
func (s *Updates) SetApplyOutcome(out ApplyOutcome) { s.applyOutcome = out }

// SetSoftwareSource 注入厂→WAN 拉包；测试用内存源，生产由通道挂上。
func (s *Updates) SetSoftwareSource(src SoftwareSource) { s.softwareSource = src }

// SetRunningSoftware 记下本进程厂服务版本，打开厂库后由 hub 传入；不写库。
func (s *Updates) SetRunningSoftware(code int64, name string) {
	// 记下版本号，判断新旧时已装不低于它。
	s.runningCode = code
	// 记下版本名，库里没这版副本时给人看。
	s.runningName = name
}

// installedBaseline 厂服务已装基线：库里已装与本进程版本取大。
func (s *Updates) installedBaseline(ctx context.Context) (int64, error) {
	// 读本厂已确认安装的版本，没有则是零。
	installed, err := s.store.InstalledSoftware(ctx, SoftwareFactoryService)
	// 已装版本读失败则不能判断新旧。
	if err != nil {
		return 0, err
	}
	// 新装厂没记已装，正在跑的版本也算装上了，免得提示同版或旧版。
	return max(installed, s.runningCode), nil
}

// completeReplica 本厂已收且摘要对得上的那一份；对象缺失或摘要不符当没有。
// 存储瞬时故障原样交回，免得被当成缺失去 WAN 重下整包。
func (s *Updates) completeReplica(ctx context.Context, kind string, version int64) (SoftwareReplica, bool, error) {
	// 按种类和版本读本厂已收副本。
	row, err := s.store.SoftwareReplica(ctx, kind, version)
	// 找不到就换成业务上的缺失，不当成库故障。
	if errors.Is(err, domain.ErrNotFound) {
		return SoftwareReplica{}, false, nil
	}
	// 这一步失败则停下，避免带着残缺结果继续。
	if err != nil {
		return SoftwareReplica{}, false, err
	}
	// 只打开对象问长度，不整包读进内存。
	obj, err := s.blobs.Open(ctx, row.ObjectKey)
	// 对象确实没了才当不完整，重拉时补回。
	if errors.Is(err, blob.ErrNotFound) {
		return SoftwareReplica{}, false, nil
	}
	// 存储一时打不开就交回原因，不当缺失去重下。
	if err != nil {
		return SoftwareReplica{}, false, err
	}
	// 记下对象总长，核对记录要按长度比。
	size := obj.Size()
	// 问完长度就关上，连接还给存储。
	_ = obj.Close()
	// 摘要每个对象只整包核一次，核不过当不完整。
	if err := s.checkBlob(ctx, row.ObjectKey, row.Digest, size); err != nil {
		// 摘要不符或核时对象没了才当不完整，重拉时补回。
		if errors.Is(err, domain.ErrIntegrity) {
			return SoftwareReplica{}, false, nil
		}
		// 读流断了或请求取消就交回原因，下次再核。
		return SoftwareReplica{}, false, err
	}
	return row, true, nil
}

// blobCheck 是一份对象已核过的摘要和长度，长度变了就要重核。
type blobCheck struct {
	digest []byte // 核对通过时的摘要
	size   int64  // 核对时的对象总长
}

// blobVerifyWait 是首次整包核摘要的时限，存储卡住也不会一直挂着。
const blobVerifyWait = 10 * time.Minute

// 核对象摘要：记过且长度没变就放行，否则按对象键单飞整包核一次。
func (k *kernel) checkBlob(ctx context.Context, key string, want []byte, size int64) error {
	// 已核过且长度没变就不再整包读。
	if k.blobVerified(key, want, size) {
		return nil
	}
	// 同一对象只核一路，别的查包和下载等它的结果。
	ch := k.blobFly.DoChan(key, func() (any, error) {
		// 排队期间上一路已核完就不再整包读。
		if k.blobVerified(key, want, size) {
			return nil, nil
		}
		// 领头的请求断了也核完，等着的人还要结果。
		return nil, k.hashBlob(context.WithoutCancel(ctx), key, want)
	})
	// 核完或本请求先断，谁先到走谁。
	select {
	// 核完就交回结果，坏对象是完整性错误。
	case res := <-ch:
		return res.Err
	// 本请求断了就先走，核对照样跑完。
	case <-ctx.Done():
		// 交回本请求的取消原因。
		return ctx.Err()
	}
}

// 整包流式算一遍摘要，对得上才记下；坏对象交回完整性错误。
func (k *kernel) hashBlob(ctx context.Context, key string, want []byte) error {
	// 给核对定个时限，存储卡住也会收尾。
	ctx, cancel := context.WithTimeout(ctx, blobVerifyWait)
	// 核完就取消，含提前返回。
	defer cancel()
	// 另开一路流来核，不动下载那一路的偏移。
	obj, err := k.blobs.Open(ctx, key)
	// 对象没了就当包已损坏。
	if errors.Is(err, blob.ErrNotFound) {
		return domain.ErrIntegrity
	}
	// 别的打不开交回原因，下次再核。
	if err != nil {
		return err
	}
	// 核完就关上对象流。
	defer obj.Close()
	// 边读边算摘要，不整包进内存。
	sum, n, err := digest.SumReader(obj)
	// 读一半断了就交回，不记核对结果。
	if err != nil {
		return err
	}
	// 摘要对不上就是坏对象，不能发出去。
	if !digest.Equal(sum, want) {
		return domain.ErrIntegrity
	}
	// 核对通过就记下，下次直接放行。
	k.rememberBlob(key, want, n)
	return nil
}

// 看这份对象是否已按这份摘要和长度核过。
func (k *kernel) blobVerified(key string, want []byte, size int64) bool {
	// 读表前先占住，避免和记下交错。
	k.blobMu.Lock()
	// 读完就放开。
	defer k.blobMu.Unlock()
	// 取出这份对象的核对记录。
	c, ok := k.blobChecked[key]
	// 摘要和长度都对上才算核过。
	return ok && c.size == size && digest.Equal(c.digest, want)
}

// 记下这份对象已核过的摘要和长度，之后查包和下载不再整包重算。
func (k *kernel) rememberBlob(key string, sum []byte, size int64) {
	// 写表前先占住，避免并发写坏。
	k.blobMu.Lock()
	// 写完就放开。
	defer k.blobMu.Unlock()
	// 第一次记才建表。
	if k.blobChecked == nil {
		// 建一张空表，按对象键记。
		k.blobChecked = map[string]blobCheck{}
	}
	// 拷一份摘要记下，调用方事后改不到。
	k.blobChecked[key] = blobCheck{digest: append([]byte(nil), sum...), size: size}
}

// 忘掉这份对象的核对记录，清掉副本后同键再收要重核。
func (k *kernel) forgetBlob(key string) {
	// 改表前先占住，避免和读交错。
	k.blobMu.Lock()
	// 改完就放开。
	defer k.blobMu.Unlock()
	// 删掉这一条，没有也不出错。
	delete(k.blobChecked, key)
}

// 清空全部核对记录，换了对象存储后都要重核。
func (k *kernel) forgetBlobs() {
	// 改表前先占住，避免和读交错。
	k.blobMu.Lock()
	// 改完就放开。
	defer k.blobMu.Unlock()
	// 换一张空表，旧记录不再算数。
	k.blobChecked = nil
}

// EnsureSoftware 本厂没有完整副本才去 WAN 拉；同版本进行中不重下。
// 下载在后台跑、不跟着调用方取消，靠读间隔看门狗收尾；调用方按自己的 ctx 退出等待。
func (s *Updates) EnsureSoftware(ctx context.Context, kind string, version int64) error {
	// 只收厂服务和客户端包，版本还必须是正数。
	if !factoryPullKind(kind) || version < 1 {
		// 云端服务包不许收到本厂。
		if kind == SoftwareWANService {
			return domain.ErrForbidden
		}
		return domain.ErrInvalidName
	}
	// 厂服务包低于已装基线就不拉，免得每次问包都把旧包整包重下。
	if kind == SoftwareFactoryService {
		// 读已装基线，库里已装与本进程版本取大。
		installed, err := s.installedBaseline(ctx)
		// 基线读失败则不能判断新旧，先不拉。
		if err != nil {
			return err
		}
		// 比基线旧直接按过旧拒绝，不拉包也不记审计。
		if version < installed {
			return domain.ErrStaleRevision
		}
	}
	// 已有完整副本或查询失败就停，避免重复下载。
	if _, ok, err := s.completeReplica(ctx, kind, version); err != nil || ok {
		return err
	}
	// 用种类加版本当单飞键，同版本只拉一次。
	key := kind + "/" + strconv.FormatInt(version, 10)
	// 占住同版本单飞，避免两路同时去拉。
	s.softwareMu.Lock()
	// 第一次拉包才建单飞表。
	if s.softwareIn == nil {
		// 第一次拉包才建单飞表。
		s.softwareIn = map[string]*softwareWait{}
	}
	// 同一版本已有人在拉就等它，没有才由这条请求领头。
	w, ok := s.softwareIn[key]
	// 没人在拉就领头，在后台起这一次下载。
	if !ok {
		// 给这次拉取一个完成信号，后来的人等它。
		w = &softwareWait{done: make(chan struct{})}
		// 占住这个版本，后来的请求等这一次。
		s.softwareIn[key] = w
		// 下载不跟着领头请求取消，断了也能让等着的人拿到结果。
		bg := context.WithoutCancel(ctx)
		// 后台真正去源侧拉一份并收下。
		go s.runSoftwarePull(bg, key, kind, version, w)
	}
	// 放开单飞，等着的人可以看这次结果。
	s.softwareMu.Unlock()
	// 等下载结束或调用方先走，谁先到走谁。
	select {
	// 下载结束就交回这次的结果。
	case <-w.done:
		return w.err
	// 调用方先走就不再等，后台下载照常跑完。
	case <-ctx.Done():
		// 交回调用方自己的取消原因。
		return ctx.Err()
	}
}

// 后台跑一次下载，结束后先摘掉单飞占位再通知等着的人。
func (s *Updates) runSoftwarePull(ctx context.Context, key, kind string, version int64, w *softwareWait) {
	// 真正去源侧拉一份并收下。
	w.err = s.pullSoftwareOnce(ctx, kind, version)
	// 占住同版本单飞，摘占位时不和新请求交错。
	s.softwareMu.Lock()
	// 拉取结束就拿走单飞占位，下次可以再拉。
	delete(s.softwareIn, key)
	// 放开单飞，新请求可以领头。
	s.softwareMu.Unlock()
	// 通知等着的人这次拉取已经结束。
	close(w.done)
}

// 真正去源侧拉一份并落副本；调用方已占住单飞。
func (s *Updates) pullSoftwareOnce(ctx context.Context, kind string, version int64) error {
	// 已有完整副本或查询失败就停，避免重复下载。
	if _, ok, err := s.completeReplica(ctx, kind, version); err != nil || ok {
		return err
	}
	// 没有拉包来源则当没有这份，不假装已收下。
	if s.softwareSource == nil {
		return domain.ErrNotFound
	}
	// 先留空版本名，问到最高版再填。
	var name string
	// 先留空摘要，问到源侧摘要再用来核对。
	var want []byte
	// 问到的最高版正是要拉的，才记下名字和摘要。
	if meta, err := s.softwareSource.Latest(ctx, kind); err == nil && meta.Version == version {
		// 问到的最高版就是要拉的，记下给人看的版本名。
		name = meta.VersionName
		// 记下源侧摘要，拉回来必须对上。
		want = meta.Digest
	}
	// 按版本把包字节拉回，带上源侧摘要，下完就核。
	body, err := s.softwareSource.Pull(ctx, kind, version, want)
	// 包字节拉不下来则停下。
	if err != nil {
		return err
	}
	// 拉回的字节和源侧摘要对不上则拒绝收下。
	if len(want) > 0 && !digest.Match(body, want) {
		return domain.ErrIntegrity
	}
	// 算出拉回字节的摘要，收下和清暂存都按它。
	sum := digest.Sum(body)
	// 摘要核对后把包收进本厂，不验签名；没收下暂存留着，重试不必重下。
	if err := s.IngestSoftware(ctx, SoftwareOffer{
		// 种类、版本、版本名、摘要和字节一起交给收下。
		Kind: kind, Version: version, VersionName: name, Digest: sum, Body: body,
	}); err != nil {
		return err
	}
	// 收下了才让源侧清掉暂存，免得一直占盘。
	s.softwareSource.Received(kind, version, sum)
	return nil
}

// SetPendingSink 生产写入更新目录；测试可空。
func (s *Updates) SetPendingSink(sink PendingSink) { s.pendingSink = sink }

// MarkInstalled updater 探活通过后记已装；已是该版本则幂等。
func (s *Updates) MarkInstalled(ctx context.Context, kind string, version int64) error {
	// 拼种类和版本，审计能对上是哪一个包。
	target := softwareTarget(kind, version)
	// 已装版本写失败则记拒绝。
	if err := s.store.PutInstalledSoftware(ctx, kind, version); err != nil {
		// 已是这个版本则幂等返回，不再记失败。
		if errors.Is(err, domain.ErrStaleRevision) {
			return nil
		}
		// 记已装失败则补记拒绝，不假装已经装上。
		_ = s.audit(ctx, nil, nil, "apply_software", target, audit.Deny)
		return err
	}
	// 记下成功；审计没写上则这次不算完成。
	return s.audit(ctx, nil, nil, "apply_software", target, audit.Allow)
}

// MarkApplyFailed updater 换包失败或已切回上一版时记一条落地拒绝；同一次结果只记一次。
// 最近一次确认之后已记过就跳过，重启和反复轮询都不重记；再确认后再失败另记一条。
func (s *Updates) MarkApplyFailed(ctx context.Context, kind string, version int64, reason string) error {
	// 缺种类或版本就对不上是哪次确认，不记。
	if kind == "" || version < 1 {
		return nil
	}
	// 确认审计的对象，用来找最近一次确认。
	confirmed := softwareTarget(kind, version)
	// updater 没给原因也写明不详，对象不留空尾巴。
	if strings.TrimSpace(reason) == "" {
		// 原因空着时用固定的不详字样。
		reason = "unknown"
	}
	// 查重和写入串起来，免得并发轮询各记一条。
	s.applyMu.Lock()
	// 记完或查失败都放开，下一次查进度才能进来。
	defer s.applyMu.Unlock()
	// 看最近一次确认之后是否已经记过这次失败。
	done, err := s.store.AuditDeniedSince(ctx, "apply_software", confirmed+":", "confirm_software", confirmed)
	// 查不了或已经记过都不再写，查不了留给下次再补。
	if err != nil || done {
		return err
	}
	// 对象带上 updater 给的原因，事后能查到为何没换上或已切回。
	return s.audit(ctx, nil, nil, "apply_software", confirmed+": "+reason, audit.Deny)
}

// SettleStaged 新进程起来后按 updater 结果补记：成功记已装，失败记落地失败。
// 没有结果或还在换都不记；补记没写上就交回错误，查进度时还会再补。
func (s *Updates) SettleStaged(ctx context.Context) error {
	// 没有进度来源就无从补记。
	if s.applyReporter == nil {
		return nil
	}
	// 读本机更换结果，种类和版本以 updater 记的为准。
	phase, kind, version, errMsg, err := s.applyReporter.Progress()
	// 结果读不了就交回，调用方只警告不拦启动。
	if err != nil {
		return err
	}
	// 缺种类或版本就对不上是哪一包，不记。
	if kind == "" || version < 1 {
		return nil
	}
	// 按成败分别补记，正在换或闲着都不动。
	switch phase {
	// 探活通过才记已装，已是该版本则幂等。
	case "ok":
		// 补记已装，晚到的就绪也能补上。
		return s.MarkInstalled(ctx, kind, version)
	// 换包失败或已切回上一版，补一条落地拒绝。
	case "fail":
		// 补记落地失败，同一次结果只记一条。
		return s.MarkApplyFailed(ctx, kind, version, errMsg)
	// 正在换或闲着，等出结果再说。
	default:
		return nil
	}
}

// IngestSoftware 收下已由本厂会话拉回的包；只核摘要，不验签名。
func (s *Updates) IngestSoftware(ctx context.Context, offer SoftwareOffer) error {
	// 拼种类和版本，审计能对上是哪一个包。
	target := softwareTarget(offer.Kind, offer.Version)
	// 种类或版本不合法则拒绝收下。
	if !factoryPullKind(offer.Kind) || offer.Version < 1 {
		// 拉包被拒时补记审计，写失败仍不收。
		_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
		// 云端包即使送到也拒绝，本厂不能收。
		if offer.Kind == SoftwareWANService {
			return domain.ErrForbidden
		}
		return domain.ErrInvalidName
	}
	// 摘要对不上当被篡改，拒绝继续使用。
	if !digest.Match(offer.Body, offer.Digest) {
		// 拉包被拒时补记审计，写失败仍不收。
		_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
		return domain.ErrIntegrity
	}
	// 已有副本先对摘要，避免不同正文覆盖对象。
	got, err := s.store.SoftwareReplica(ctx, offer.Kind, offer.Version)
	// 已有同版本则对摘要，避免不同正文覆盖对象。
	if err == nil {
		// 摘要对不上当被篡改，拒绝继续使用。
		if !digest.Match(offer.Body, got.Digest) {
			// 拉包被拒时补记审计，写失败仍不收。
			_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
			return domain.ErrIntegrity
		}
		// 同摘要再收把对象补回，避免进程重启丢内存存根后确认失败。
		if err := s.blobs.Put(ctx, got.ObjectKey, offer.Body); err != nil {
			// 拉包被拒时补记审计，写失败仍不收。
			_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
			return err
		}
		// 刚补回的就是这份摘要，查包和下载不必再整包核。
		s.rememberBlob(got.ObjectKey, got.Digest, int64(len(offer.Body)))
		// 记下成功；审计没写上则这次不算完成。
		return s.audit(ctx, nil, nil, "pull_software", target, audit.Allow)
	}
	// 不是找不到则把库错误抛回，避免当成没有。
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	// 厂服务包不能比本厂已装的版本更旧。
	if offer.Kind == SoftwareFactoryService {
		// 读已装基线，库里已装与本进程版本取大。
		installed, err := s.installedBaseline(ctx)
		// 已装版本读失败则不能判断新旧。
		if err != nil {
			return err
		}
		// 比已装还旧则拒绝，避免确认时倒退。
		if offer.Version < installed {
			// 拉包被拒时补记审计，写失败仍不收。
			_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
			return domain.ErrStaleRevision
		}
	}
	// 读该种类已收的最高版本。
	max, err := s.store.MaxSoftwareReplica(ctx, offer.Kind)
	// 最高版本读失败则不能比较新旧。
	if err != nil {
		return err
	}
	// 比已收最高还旧则拒绝，不把旧包插回来。
	if offer.Version < max {
		// 拉包被拒时补记审计，写失败仍不收。
		_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
		return domain.ErrStaleRevision
	}
	// 按种类和版本生成对象键，避免不同包写到一处。
	key := store.SoftwareObjectKey(offer.Kind, offer.Version)
	// 对象写失败则记拒绝。
	if err := s.blobs.Put(ctx, key, offer.Body); err != nil {
		// 拉包被拒时补记审计，写失败仍不收。
		_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
		return err
	}
	// 刚写进去的就是这份摘要，查包和下载不必再整包核。
	s.rememberBlob(key, offer.Digest, int64(len(offer.Body)))
	// 对象落好后再记副本行，写失败则这次不算收下。
	if _, err := s.store.InsertSoftwareReplica(ctx, SoftwareReplica{
		// 记下种类、版本和给人看的版本名。
		Kind: offer.Kind, Version: offer.Version, VersionName: offer.VersionName,
		Digest: offer.Digest, ObjectKey: key,
	}); err != nil {
		_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
		return err
	}
	// 记下成功；审计没写上则这次不算完成。
	return s.audit(ctx, nil, nil, "pull_software", target, audit.Allow)
}

// SyncFactorySoftware 超管触发问最高版并静默拉；已有完整副本则不再下。
func (s *Updates) SyncFactorySoftware(ctx context.Context, token, kind string) (SoftwareReplica, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return SoftwareReplica{}, err
	}
	// 拼种类和版本，审计能对上是哪一个包。
	target := softwareTarget(kind, 0)
	// 当前人无权则记拒绝，不继续往下改。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 拉包被拒时补记审计，写失败仍不收。
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", target, audit.Deny)
		return SoftwareReplica{}, err
	}
	// 超管也只能同步厂服务或客户端包。
	if !factoryPullKind(kind) {
		// 拉包被拒时补记审计，写失败仍不收。
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", kind, audit.Deny)
		return SoftwareReplica{}, domain.ErrForbidden
	}
	// 没有通往云端的拉包来源则拒绝同步。
	if s.softwareSource == nil {
		// 拉包被拒时补记审计，写失败仍不收。
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", kind, audit.Deny)
		return SoftwareReplica{}, domain.ErrNotFound
	}
	// 问云端该种类当前最高版本，先不拿包体。
	meta, err := s.softwareSource.Latest(ctx, kind)
	// 失败则记拒绝并停下，不继续往下改。
	if err != nil {
		// 拉包被拒时补记审计，写失败仍不收。
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", kind, audit.Deny)
		return SoftwareReplica{}, err
	}
	// 只问最高版；正文有完整副本就不再下。
	if err := s.EnsureSoftware(ctx, meta.Kind, meta.Version); err != nil {
		return SoftwareReplica{}, err
	}
	// 看本厂是否已有摘要对得上的完整副本。
	row, ok, err := s.completeReplica(ctx, kind, meta.Version)
	// 副本查询失败则先停下，不重复去拉。
	if err != nil {
		return SoftwareReplica{}, err
	}
	// 拉完仍没有完整副本则当损坏，不把半份交出去。
	if !ok {
		return SoftwareReplica{}, domain.ErrIntegrity
	}
	return row, nil
}

// CurrentFactorySoftware 本厂厂服务已装基线；库里没记且本进程没报版本则为 0。
func (s *Updates) CurrentFactorySoftware(ctx context.Context, token string) (SoftwareReplica, error) {
	// 登录已失效则拒绝，避免未登录的人继续。
	if _, err := s.RequireActive(ctx, token); err != nil {
		return SoftwareReplica{}, err
	}
	// 读已装基线，库里已装与本进程版本取大。
	installed, err := s.installedBaseline(ctx)
	// 已装版本读失败则不能判断新旧。
	if err != nil {
		return SoftwareReplica{}, err
	}
	// 还没装过就返回版本为零，不当成有包。
	if installed < 1 {
		return SoftwareReplica{Kind: SoftwareFactoryService}, nil
	}
	// 按种类和版本读本厂已收副本。
	row, err := s.store.SoftwareReplica(ctx, SoftwareFactoryService, installed)
	// 找不到就换成业务上的缺失，不当成库故障。
	if errors.Is(err, domain.ErrNotFound) {
		// 没有副本时只给版本号，版本名先空着。
		cur := SoftwareReplica{Kind: SoftwareFactoryService, Version: installed}
		// 基线正是本进程版本，就用本进程的版本名给人看。
		if installed == s.runningCode {
			// 填上本进程版本名，首页不显示空名。
			cur.VersionName = s.runningName
		}
		return cur, nil
	}
	return row, err
}

// PendingFactorySoftware 超管看本厂已收完整、高于已装的厂服务包；没有则空。
func (s *Updates) PendingFactorySoftware(ctx context.Context, token string) (*SoftwareReplica, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return nil, err
	}
	// 当前人无权则记拒绝，不继续往下改。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 无权看待装包则记拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "pending_software", SoftwareFactoryService, audit.Deny)
		return nil, err
	}
	// 找高于已装、且本厂已收完整的最高包。
	row, ok, err := s.pendingReplica(ctx, SoftwareFactoryService, 0)
	// 没有完整结果或查询失败则按没有处理。
	if err != nil || !ok {
		return nil, err
	}
	return &row, nil
}

// pendingReplica 高于已装版本的最高已收副本；没有则空。
func (s *Updates) pendingReplica(ctx context.Context, kind string, installed int64) (SoftwareReplica, bool, error) {
	// 没传入已装版本时，自己去读厂服务已装到哪一版。
	if kind == SoftwareFactoryService && installed == 0 {
		// 已装版本的错误放在外面，读失败就停下。
		var err error
		// 读已装基线，库里已装与本进程版本取大。
		installed, err = s.installedBaseline(ctx)
		// 已装版本读失败则不能判断新旧。
		if err != nil {
			return SoftwareReplica{}, false, err
		}
	}
	// 读该种类已收的最高版本。
	max, err := s.store.MaxSoftwareReplica(ctx, kind)
	// 最高版本读失败则不能比较新旧。
	if err != nil {
		return SoftwareReplica{}, false, err
	}
	// 没有比已装更高的副本，就当没有待装包。
	if max <= installed {
		return SoftwareReplica{}, false, nil
	}
	// 提示只认本厂已收完整副本，不完整当没有。
	return s.completeReplica(ctx, kind, max)
}

// ConfirmFactoryUpdate 仅本厂超管确认后才写待切换；落地成功才记已装。
func (s *Updates) ConfirmFactoryUpdate(ctx context.Context, token, kind string, version int64) error {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return err
	}
	// 拼种类和版本，审计能对上是哪一个包。
	target := softwareTarget(kind, version)
	// 当前人无权则记拒绝，不继续往下改。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 确认被拒时补记审计，写失败仍不切换。
		_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
		return err
	}
	// 只能确认厂服务包，客户端包不走这里。
	if kind != SoftwareFactoryService {
		// 确认被拒时补记审计，写失败仍不切换。
		_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
		return domain.ErrForbidden
	}
	// 找高于已装、且本厂已收完整的最高包。
	pending, ok, err := s.pendingReplica(ctx, kind, 0)
	// 待装包查失败则不能确认或展示。
	if err != nil {
		return err
	}
	// 没有这份或版本对不上则拒绝确认。
	if !ok || pending.Version != version {
		// 确认被拒时补记审计，写失败仍不切换。
		_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
		// 没有这份待装包则按找不到拒绝。
		if !ok {
			return domain.ErrNotFound
		}
		return domain.ErrStaleRevision
	}
	// 从对象存储取出包字节，丢了就当不完整。
	body, err := s.blobs.Get(ctx, pending.ObjectKey)
	// 对象读不到则记拒绝，不把坏包给出去。
	if err != nil {
		// 确认被拒时补记审计，写失败仍不切换。
		_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
		// 对象丢了当完整性失败，不把空包交出去。
		if errors.Is(err, blob.ErrNotFound) {
			return domain.ErrIntegrity
		}
		return err
	}
	// 摘要对不上当被篡改，拒绝继续使用。
	if !digest.Match(body, pending.Digest) {
		// 确认被拒时补记审计，写失败仍不切换。
		_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
		return domain.ErrIntegrity
	}
	// 生产才把包写到待切换目录，测试可以不写。
	if s.pendingSink != nil {
		// 落待切换失败则记拒绝，不记已装。
		if err := s.pendingSink.Stage(kind, version, pending.Digest, body); err != nil {
			// 确认被拒时补记审计，写失败仍不切换。
			_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
			return err
		}
	}
	// 按落地结果记已装，或只保持待切换。
	return s.finishApply(ctx, &acc.ID, kind, version, target)
}

// ApplyProgress 超管看本机更换进度；就绪则补记已装。
type ApplyProgress struct {
	Phase   string `json:"phase"`           // idle / applying / ok / fail
	Kind    string `json:"kind"`            // 待切换或已落地种类
	Version int64  `json:"version"`         // 对应版本；idle 为 0
	Error   string `json:"error,omitempty"` // fail 时 updater 原因
}

// ApplyProgress 读盘上进度；探活通过才记已装，失败补记一条落地拒绝。
func (s *Updates) ApplyProgress(ctx context.Context, token string) (ApplyProgress, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return ApplyProgress{}, err
	}
	// 当前人无权则拒绝，不继续往下改。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		return ApplyProgress{}, err
	}
	// 没有进度来源就当闲置，不当成更换失败。
	if s.applyReporter == nil {
		return ApplyProgress{Phase: "idle"}, nil
	}
	// 读本机更换进度，就绪后再补记已装。
	phase, kind, version, errMsg, err := s.applyReporter.Progress()
	// 进度读失败则这次查询失败。
	if err != nil {
		return ApplyProgress{}, err
	}
	// 更换就绪就补记已装，晚到的结果也能补上。
	if phase == "ok" && kind != "" && version > 0 {
		// updater 写就绪可能晚于新进程启动，这里补记已装。
		if err := s.MarkInstalled(ctx, kind, version); err != nil {
			return ApplyProgress{}, err
		}
	}
	// 换包失败或已切回上一版就补一条落地拒绝，同一次结果只记一条。
	if phase == "fail" {
		// 审计没写上不挡页面显示失败，下次轮询再补。
		_ = s.MarkApplyFailed(ctx, kind, version, errMsg)
	}
	return ApplyProgress{Phase: phase, Kind: kind, Version: version, Error: errMsg}, nil
}

// finishApply 按夹具结果记已装或保持待切换。
func (s *Updates) finishApply(ctx context.Context, actor *uuid.UUID, kind string, version int64, target string) error {
	// 按落地结果决定记失败、记已装，或只保持待切换。
	switch s.applyOutcome {
	// 宣称落地失败则记拒绝，不改已装版本。
	case ApplyFail:
		// 确认被拒时补记审计，写失败仍不切换。
		_ = s.audit(ctx, actor, nil, "confirm_software", target, audit.Deny)
		return domain.ErrSoftwareInstallFailed
	// 宣称落地成功才把已装版本记上。
	case ApplyOK:
		// 已装版本写失败则记拒绝。
		if err := s.store.PutInstalledSoftware(ctx, kind, version); err != nil {
			// 确认被拒时补记审计，写失败仍不切换。
			_ = s.audit(ctx, actor, nil, "confirm_software", target, audit.Deny)
			return err
		}
		// 记下成功；审计没写上则这次不算完成。
		return s.audit(ctx, actor, nil, "confirm_software", target, audit.Allow)
	// 生产默认只记待切换已安排，已装等探活后再补。
	default:
		// 记下成功；审计没写上则这次不算完成。
		return s.audit(ctx, actor, nil, "confirm_software", target, audit.Allow)
	}
}

// ConfirmClientUpdate 当前登录者确认后才记下本机已装版本；焊接中拒绝。
func (s *Updates) ConfirmClientUpdate(ctx context.Context, bag *Bag, clocks Clocks, version int64) error {
	// 拼种类和版本，审计能对上是哪一个包。
	target := softwareTarget(SoftwareClientAPK, version)
	// 在线记服务器时间，离线记本机时间。
	src := bagTimeSource(*bag)
	// 没有登录者则拒绝确认客户端更新。
	if bag.OperatorID == nil {
		// 确认被拒时补记审计，写失败仍不切换。
		_ = s.auditTimed(ctx, nil, nil, "confirm_software", target, audit.Deny, src)
		return domain.ErrForbidden
	}
	// 正在焊接则拒绝换客户端，避免作业中断。
	if bag.Welding {
		// 确认被拒时补记审计，写失败仍不切换。
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, src)
		return domain.ErrForbidden
	}
	// 按本机时钟和状态看现在允不允许操作。
	node := EvaluateRuntime(*bag, clocks, NodeOpen)
	// 运行状态不允许时拒绝确认。
	if node.Decision != NodeAllow {
		// 确认被拒时补记审计，写失败仍不切换。
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, node.TimeSource)
		return domain.ErrForbidden
	}
	// 对不上本厂账号则拒绝，并补记审计。
	if _, err := s.operatorAccount(ctx, *bag); err != nil {
		// 确认被拒时补记审计，写失败仍不切换。
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, src)
		return err
	}
	// 不比本机已装更新则拒绝，避免装回旧版。
	if version <= bag.SoftwareVersion {
		// 确认被拒时补记审计，写失败仍不切换。
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, src)
		return domain.ErrStaleRevision
	}
	// 找高于已装、且本厂已收完整的最高包。
	pending, ok, err := s.pendingReplica(ctx, SoftwareClientAPK, bag.SoftwareVersion)
	// 待装包查失败则不能确认或展示。
	if err != nil {
		return err
	}
	// 没有这份或版本对不上则拒绝确认。
	if !ok || pending.Version != version {
		// 确认被拒时补记审计，写失败仍不切换。
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, src)
		// 没有这份客户端包则按找不到拒绝。
		if !ok {
			return domain.ErrNotFound
		}
		return domain.ErrStaleRevision
	}
	// 确认之后本机记下已装的客户端版本。
	bag.SoftwareVersion = version
	// 记下成功；审计没写上则这次不算完成。
	return s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Allow, src)
}

// PadClientSoftware 登录者看本厂已收最高客户端包元数据，不含字节。
func (s *Updates) PadClientSoftware(ctx context.Context, token string) (*SoftwareReplica, error) {
	// 登录已失效则拒绝，避免未登录的人继续。
	if _, err := s.RequireActive(ctx, token); err != nil {
		return nil, err
	}
	// 读该种类已收的最高版本。
	max, err := s.store.MaxSoftwareReplica(ctx, SoftwareClientAPK)
	// 最高版本读失败则不能比较新旧。
	if err != nil {
		return nil, err
	}
	// 本厂还没收过客户端包，就返回空。
	if max < 1 {
		return nil, nil
	}
	// 看本厂是否已有摘要对得上的完整副本。
	row, ok, err := s.completeReplica(ctx, SoftwareClientAPK, max)
	// 没有完整结果或查询失败则按没有处理。
	if err != nil || !ok {
		return nil, err
	}
	return &row, nil
}

// SoftwareStream 是按版本打开的一份软件包流，只给已登录拉取方；用完要关。
type SoftwareStream struct {
	Kind        string      // 固定 client_apk
	Version     int64       // 单调整数
	VersionName string      // 给人看的版本名
	Digest      []byte      // 包文件 SHA-256
	Body        blob.Object // 包字节流，可定位续传，调用方负责关
}

// PullPadClientSoftware 登录者按版本打开客户端包流，不整包进内存；摘要不对不给。
// 摘要每个对象只整包核一次；resume 为真是续传请求，不再记允许审计。
func (s *Updates) PullPadClientSoftware(ctx context.Context, token string, version int64, resume bool) (SoftwareStream, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 拼种类和版本，审计能对上是哪一个包。
	target := softwareTarget(SoftwareClientAPK, version)
	// 失败则记拒绝并停下，不继续往下改。
	if err != nil {
		// 拉包被拒时补记审计，写失败仍不收。
		_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
		return SoftwareStream{}, err
	}
	// 版本不是正数则拒绝把包给平板。
	if version < 1 {
		// 拉包被拒时补记审计，写失败仍不收。
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", target, audit.Deny)
		return SoftwareStream{}, domain.ErrInvalidName
	}
	// 按种类和版本读本厂已收副本。
	row, err := s.store.SoftwareReplica(ctx, SoftwareClientAPK, version)
	// 失败则记拒绝并停下，不继续往下改。
	if err != nil {
		// 拉包被拒时补记审计，写失败仍不收。
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", target, audit.Deny)
		return SoftwareStream{}, err
	}
	// 打开对象流，只问长度，不整包读进内存。
	obj, err := s.blobs.Open(ctx, row.ObjectKey)
	// 对象打不开则记拒绝，不把坏包给出去。
	if err != nil {
		// 拉包被拒时补记审计，写失败仍不收。
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", target, audit.Deny)
		// 对象丢了当完整性失败，不把空包交出去。
		if errors.Is(err, blob.ErrNotFound) {
			return SoftwareStream{}, domain.ErrIntegrity
		}
		return SoftwareStream{}, err
	}
	// 摘要没核过就先整包核一遍，坏对象不发出去。
	if err := s.checkBlob(ctx, row.ObjectKey, row.Digest, obj.Size()); err != nil {
		// 不发了就关上对象流。
		_ = obj.Close()
		// 拉包被拒时补记审计，写失败仍不收。
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", target, audit.Deny)
		return SoftwareStream{}, err
	}
	// 从头拉才记允许，断线续传不重复记。
	if !resume {
		// 成功必须记上审计，没记上则本次不算做成。
		if err := s.audit(ctx, &acc.ID, nil, "pull_software", target, audit.Allow); err != nil {
			// 不发了就关上对象流。
			_ = obj.Close()
			return SoftwareStream{}, err
		}
	}
	return SoftwareStream{
		Kind: row.Kind, Version: row.Version, VersionName: row.VersionName,
		Digest: row.Digest, Body: obj,
	}, nil
}

const (
	SoftwareKeepLatest    = "latest"    // 该种类当前最高，提示或平板还要用
	SoftwareKeepInstalled = "installed" // 本厂正在跑
)

// SoftwareItem 是带保留原因的本厂副本行。
type SoftwareItem struct {
	SoftwareReplica        // 嵌入的本厂软件副本
	Keep            string `json:"keep"` // latest / installed / 空则可清
}

// ImagePrune 是本机清镜像进度。
type ImagePrune struct {
	Ready     bool   `json:"ready"`               // updater 已写结果
	OK        bool   `json:"ok"`                  // 清完
	Reclaimed string `json:"reclaimed,omitempty"` // 回报空间
}

// ListFactorySoftware 超管看本厂已收副本，按种类分列。
func (s *Updates) ListFactorySoftware(ctx context.Context, token, kind string) ([]SoftwareItem, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return nil, err
	}
	// 当前人无权则记拒绝，不继续往下改。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 无权看软件副本则记拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "list_software", kind, audit.Deny)
		return nil, err
	}
	// 只列厂服务和客户端包，别的种类拒绝。
	if kind != "" && !factoryPullKind(kind) {
		return nil, domain.ErrInvalidName
	}
	// 列出本厂该种类已收副本。
	rows, err := s.store.ListSoftwareReplicas(ctx, kind)
	// 副本列表读失败则不能展示或清理。
	if err != nil {
		return nil, err
	}
	// 按副本份数准备列表，并标上能否删除。
	out := make([]SoftwareItem, 0, len(rows))
	// 每份副本标上该留还是可以清。
	for _, row := range rows {
		// 标出正在用的和当前最高，这两份不能删。
		keep, err := s.softwareKeep(ctx, row.Kind, row.Version)
		// 保留原因算不清则先不删。
		if err != nil {
			return nil, err
		}
		// 带上保留原因，最高和已装的不能清。
		out = append(out, SoftwareItem{SoftwareReplica: row, Keep: keep})
	}
	return out, nil
}

// 已装和当前最高必须留着。
func (s *Updates) softwareKeep(ctx context.Context, kind string, version int64) (string, error) {
	// 读该种类已收的最高版本。
	max, err := s.store.MaxSoftwareReplica(ctx, kind)
	// 最高版本读失败则不能比较新旧。
	if err != nil {
		return "", err
	}
	// 客户端包没有已装版本，先按零。
	installed := int64(0)
	// 厂服务包还要看正在跑的版本，客户端包不看。
	if kind == SoftwareFactoryService {
		// 读已装基线，库里已装与本进程版本取大。
		installed, err = s.installedBaseline(ctx)
		// 已装版本读失败则不能判断新旧。
		if err != nil {
			return "", err
		}
	}
	// 正在跑的这一版必须留下，不能清。
	if version == installed && installed > 0 {
		return SoftwareKeepInstalled, nil
	}
	// 当前最高版必须留下，提示和平板还要用。
	if version == max && max > 0 {
		return SoftwareKeepLatest, nil
	}
	return "", nil
}

// DeleteFactorySoftware 清掉一份不是最高也不是已装的本厂旧副本。
func (s *Updates) DeleteFactorySoftware(ctx context.Context, token, kind string, version int64) error {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return err
	}
	// 拼种类和版本，审计能对上是哪一个包。
	target := softwareTarget(kind, version)
	// 当前人无权则记拒绝，不继续往下改。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 这包不能删或删除失败时记拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Deny)
		return err
	}
	// 种类或版本不合法则拒绝删除。
	if !factoryPullKind(kind) || version < 1 {
		// 这包不能删或删除失败时记拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Deny)
		return domain.ErrInvalidName
	}
	// 标出正在用的和当前最高，这两份不能删。
	keep, err := s.softwareKeep(ctx, kind, version)
	// 保留原因算不清则先不删。
	if err != nil {
		return err
	}
	// 正在用或当前最高的包拒绝删除。
	if keep != "" {
		// 这包不能删或删除失败时记拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Deny)
		return domain.ErrReferenced
	}
	// 按种类和版本读本厂已收副本。
	row, err := s.store.SoftwareReplica(ctx, kind, version)
	// 失败则记拒绝并停下，不继续往下改。
	if err != nil {
		// 这包不能删或删除失败时记拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Deny)
		return err
	}
	// 对象删失败则记拒绝，记录先留着。
	if err := s.blobs.Delete(ctx, row.ObjectKey); err != nil {
		// 这包不能删或删除失败时记拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Deny)
		return err
	}
	// 对象已删，核对记录跟着作废。
	s.forgetBlob(row.ObjectKey)
	// 副本行删失败则记拒绝。
	if err := s.store.DeleteSoftwareReplica(ctx, kind, version); err != nil {
		// 这包不能删或删除失败时记拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Deny)
		return err
	}
	// 删掉之后记成功；审计失败则调用方当没删成。
	return s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Allow)
}

// PruneFactorySoftware 清掉该种类所有可删旧副本；kind 空则两类都清。
func (s *Updates) PruneFactorySoftware(ctx context.Context, token, kind string) (int, error) {
	// 先列出可删和必须留的副本。
	items, err := s.ListFactorySoftware(ctx, token, kind)
	// 列表失败则一次都不删，避免误清。
	if err != nil {
		return 0, err
	}
	// 记下这次真正删掉的份数。
	n := 0
	// 必须留的跳过，可清的逐个删。
	for _, item := range items {
		// 必须留下的跳过，只删可以清的旧包。
		if item.Keep != "" {
			continue
		}
		// 这一步失败则停下，避免带着残缺结果继续。
		if err := s.DeleteFactorySoftware(ctx, token, item.Kind, item.Version); err != nil {
			return n, err
		}
		// 这一份已经删掉，计数加一。
		n++
	}
	return n, nil
}

// RequestImagePrune 请本机 updater 清无用 app 镜像；ref 空则全部可清。
func (s *Updates) RequestImagePrune(ctx context.Context, token, ref string) error {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return err
	}
	// 点名清用标签，一键清用固定对象，方便对审计。
	target := imagePruneAuditTarget(ref)
	// 当前人无权则记拒绝，不继续往下改。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 清镜像被拒时补记审计，写失败仍不请清。
		_ = s.audit(ctx, &acc.ID, nil, "prune_images", target, audit.Deny)
		return err
	}
	// 失败则记拒绝并停下，不继续往下改。
	if err := s.imagePruneTarget(ref); err != nil {
		// 清镜像被拒时补记审计，写失败仍不请清。
		_ = s.audit(ctx, &acc.ID, nil, "prune_images", target, audit.Deny)
		return err
	}
	// 没有清理器就只记一笔成功，不真的去清。
	if s.imageJanitor == nil {
		// 请求已收下就记成功；没记上则不算请完。
		return s.audit(ctx, &acc.ID, nil, "prune_images", target, audit.Allow)
	}
	// 清理请求失败则记拒绝。
	if err := s.imageJanitor.RequestPrune(ref); err != nil {
		// 清镜像被拒时补记审计，写失败仍不请清。
		_ = s.audit(ctx, &acc.ID, nil, "prune_images", target, audit.Deny)
		return err
	}
	// 请求已收下就记成功；没记上则不算请完。
	return s.audit(ctx, &acc.ID, nil, "prune_images", target, audit.Allow)
}

// 点名清某一个标签时，current / previous 拒绝。
func (s *Updates) imagePruneTarget(ref string) error {
	// 不点名则清全部可清的，不再逐条拦。
	if ref == "" {
		return nil
	}
	// 标签格式不合法则拒绝，挡住命令字符。
	if !validImageRef(ref) {
		return domain.ErrInvalidName
	}
	// 没有清单就无法点名判断，当作可以请清。
	if s.imageJanitor == nil {
		return nil
	}
	// 读本机镜像清单，用来判断哪些能清。
	raw, err := s.imageJanitor.ImagesJSON()
	// 镜像清单读失败则不能点名清理。
	if err != nil {
		return err
	}
	// 还没有镜像清单则按找不到拒绝点名。
	if len(raw) == 0 {
		return domain.ErrNotFound
	}
	// 准备接镜像清单，解析失败则不能点名清。
	var usage ImageUsage
	// 这一步失败则停下，避免带着残缺结果继续。
	if err := json.Unmarshal(raw, &usage); err != nil {
		return err
	}
	// 找到点名的标签；正在用的不能清。
	for _, it := range usage.Items {
		// 不是点名的那条就继续找。
		if it.Ref != ref {
			continue
		}
		// 正在用或上一版拒绝清理。
		if it.Keep != "" {
			return domain.ErrReferenced
		}
		return nil
	}
	return domain.ErrNotFound
}

// 镜像标签只允许仓库名加版本，挡住命令字符。
func validImageRef(ref string) bool {
	// 空标签表示一键清，格式上放行。
	if ref == "" {
		return true
	}
	// 过长或带命令字符的标签拒绝，避免被当成命令。
	if len(ref) > 256 || strings.ContainsAny(ref, " \t\n\r;|&$`'\"\\<>") {
		return false
	}
	// 标签必须是仓库名加一个版本，才允许点名清。
	host, tag, ok := strings.Cut(ref, ":")
	// 缺版本、多冒号或空仓库名都拒绝，挡住命令字符。
	return ok && host != "" && tag != "" && !strings.Contains(tag, ":")
}

// 审计对象：点名用标签，一键清用 docker。
func imagePruneAuditTarget(ref string) string {
	// 一键清用固定对象记审计，点名才用标签。
	if ref == "" {
		return "docker"
	}
	return ref
}

// ImagePruneProgress 超管看 updater 清镜像结果。
func (s *Updates) ImagePruneProgress(ctx context.Context, token string) (ImagePrune, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return ImagePrune{}, err
	}
	// 当前人无权则拒绝，不继续往下改。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		return ImagePrune{}, err
	}
	// 没有清理器就当已经清完，回收为零。
	if s.imageJanitor == nil {
		return ImagePrune{Ready: true, OK: true, Reclaimed: "0B"}, nil
	}
	// 读清镜像的结果，没有就当还没写完。
	reclaimed, ok, present, err := s.imageJanitor.PruneResult()
	// 结果读失败则进度查询失败。
	if err != nil {
		return ImagePrune{}, err
	}
	return ImagePrune{Ready: present, OK: ok, Reclaimed: reclaimed}, nil
}

// BlobUsage 是对象存储已用，没有配额。
type BlobUsage struct {
	Used    int64  `json:"used"`             // 对象字节合计
	Objects int64  `json:"objects"`          // 对象个数
	Bucket  string `json:"bucket,omitempty"` // 桶名；内存存根为空
}

// ImageItem 是本机一条 app 镜像标签。
type ImageItem struct {
	Ref  string `json:"ref"`  // repository:tag
	ID   string `json:"id"`   // docker 镜像短号
	Size int64  `json:"size"` // 该标签字节
	Keep string `json:"keep"` // current / previous / 空则可清
}

// ImageUsage 是本机 app 镜像占用，由 updater 写入。
type ImageUsage struct {
	Kind  string      `json:"kind"`  // 本机服务种类
	Used  int64       `json:"used"`  // 去重后字节
	Count int         `json:"count"` // 去重后个数
	Items []ImageItem `json:"items"` // 标签列表
}

// StorageUsage 是本侧磁盘、对象存储、库和本机 app 镜像占用。
type StorageUsage struct {
	Disk     disk.Space `json:"disk"`     // 本机根盘
	OSS      BlobUsage  `json:"oss"`      // 本侧桶内对象
	Database int64      `json:"database"` // 当前厂库字节
	Images   ImageUsage `json:"images"`   // 本机 app 镜像
}

// StorageUsage 超管看本机磁盘余量、对象存储已用、本厂库占用和本机 app 镜像。
func (s *Updates) StorageUsage(ctx context.Context, token string) (StorageUsage, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return StorageUsage{}, err
	}
	// 当前人无权则拒绝，不继续往下改。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		return StorageUsage{}, err
	}
	// 先放空占用，读到哪项填哪项。
	out := StorageUsage{}
	// 根盘余量读到了就填上，读不到留空。
	if space, err := disk.Of("/"); err == nil {
		// 根盘余量读到了就填上。
		out.Disk = space
	}
	// 有对象存储才统计用量，没有就跳过。
	if s.blobs != nil {
		// 统计本侧对象存储已用字节和个数。
		used, objects, err := s.blobs.Usage(ctx)
		// 用量读失败则整份占用不返回。
		if err != nil {
			return StorageUsage{}, err
		}
		// 填上对象存储已用字节和个数。
		out.OSS = BlobUsage{Used: used, Objects: objects}
		if named, ok := s.blobs.(interface{ Bucket() string }); ok { // 只有真桶才有名字，内存存根跳过
			out.OSS.Bucket = named.Bucket()
		}
	}
	// 读当前厂库占用的字节。
	n, err := s.store.DatabaseSize(ctx)
	// 库大小读失败则整份占用不返回。
	if err != nil {
		return StorageUsage{}, err
	}
	// 填上当前厂库占用的字节。
	out.Database = n
	// 有镜像清理器才读本机镜像占用。
	if s.imageJanitor != nil {
		// 读本机镜像清单，用来判断哪些能清。
		raw, err := s.imageJanitor.ImagesJSON()
		// 镜像清单读失败则不能点名清理。
		if err != nil {
			return StorageUsage{}, err
		}
		// 清单是空的就不解析，避免把空内容当失败。
		if len(raw) > 0 {
			// 这一步失败则停下，避免带着残缺结果继续。
			if err := json.Unmarshal(raw, &out.Images); err != nil {
				return StorageUsage{}, err
			}
		}
	}
	// 没有镜像时给空列表，避免调用方拿到空值。
	if out.Images.Items == nil {
		// 没有镜像时给空列表，避免前端拿到空值。
		out.Images.Items = []ImageItem{}
	}
	return out, nil
}
