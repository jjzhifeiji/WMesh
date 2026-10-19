// Package appupdate 把已确认的 app 包落到更新目录，供本机 updater 来换容器。
// 不管 Docker，也不做业务判定。
package appupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"wmesh/factory/internal/platform/domain"
)

const (
	tarFile     = "pending.tar"  // 确认后落下的镜像包
	pendingFile = "pending.json" // 种类、版本、摘要
	statusFile  = "status.json"  // updater 写就绪或失败
	pruneReq    = "prune.req"    // 请 updater 清无用镜像
	pruneFile   = "prune.json"   // updater 写清镜像结果
	imagesFile  = "images.json"  // updater 写本机 app 镜像占用
)

// stageMu 让同进程的几次确认排队落包，免得交错写成半份。
var stageMu sync.Mutex

// Dir 把确认后的包写到 dir。
type Dir string

// 待切换的种类、版本和摘要。
type pendingMeta struct {
	Kind    string `json:"kind"`    // factory_service
	Version int64  `json:"version"` // 待切换版本
	Digest  []byte `json:"digest"`  // SHA-256
}

// 换包结果，成功还是失败原因，以及换的是哪一版。
type statusMeta struct {
	OK      bool   `json:"ok"`                // 探活通过
	Kind    string `json:"kind,omitempty"`    // updater 开工时读到的种类
	Version int64  `json:"version,omitempty"` // updater 开工时读到的版本；旧格式为 0
	Digest  []byte `json:"digest,omitempty"`  // updater 开工时读到的摘要，便于对账
	Error   string `json:"error,omitempty"`   // 失败原因，只写进落地失败审计
}

// Stage 只写 tar 和待切换说明，不换容器；上一包还没出结果就拒绝。
func (d Dir) Stage(kind string, version int64, digest, body []byte) error {
	// 收成普通文本，方便当路径或名字用。
	dir := string(d)
	// 目录还空着就改用本机默认的更新位置。
	if dir == "" {
		// 没给目录时用本机默认的更新位置。
		dir = "/var/lib/wmesh/update"
	}
	// 同进程的几次确认排队落包，免得交错写。
	stageMu.Lock()
	// 落完或中途失败都放开，下一次确认才能进来。
	defer stageMu.Unlock()
	// 没能先把目录准备出来就停，避免带着残缺继续。
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// 看 updater 是否还在排队或正在换上一包。
	busy, err := applyingNow(dir)
	// 判不清就停，不冒险覆盖正在用的包。
	if err != nil {
		return err
	}
	// 上一包还没出结果就拒绝，免得结果记到新版本头上。
	if busy {
		return domain.ErrSoftwareApplying
	}
	// 先删待切换说明，updater 就不会拿旧说明配上新旧不明的包。
	if err := removeIfAny(filepath.Join(dir, pendingFile)); err != nil {
		return err
	}
	// 再清掉上一轮结果，避免把旧成败当成这次。
	if err := removeIfAny(filepath.Join(dir, statusFile)); err != nil {
		return err
	}
	// 拼出同一目录下的路径。
	tmp := filepath.Join(dir, tarFile+".tmp")
	// 镜像包没读好或没写好就停，避免往下用残缺结果。
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	// 镜像包没读好或没写好就停，避免往下用残缺结果。
	if err := os.Rename(tmp, filepath.Join(dir, tarFile)); err != nil {
		return err
	}
	// 把结构收成字节，再交给后面。
	raw, err := json.Marshal(pendingMeta{Kind: kind, Version: version, Digest: digest})
	// 没能把结构收成字节就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 拼出同一目录下的路径。
	metaTmp := filepath.Join(dir, pendingFile+".tmp")
	// 待切换清单没读好或没写好就停，避免往下用残缺结果。
	if err := os.WriteFile(metaTmp, raw, 0o600); err != nil {
		return err
	}
	// 换名成功才算写完，读的人不会看见半截。
	return os.Rename(metaTmp, filepath.Join(dir, pendingFile))
}

// Progress 读待切换与 updater 结果：idle / applying / ok / fail；结果带版本就按结果报。
func (d Dir) Progress() (phase, kind string, version int64, errMsg string, err error) {
	// 收成普通文本，方便当路径或名字用。
	dir := string(d)
	// 目录还空着就改用本机默认的更新位置。
	if dir == "" {
		// 没给目录时用本机默认的更新位置。
		dir = "/var/lib/wmesh/update"
	}
	// 读出待切换的清单。
	meta, hasPending, err := readPending(dir)
	// 没能读出待切换的清单就停，避免带着残缺继续。
	if err != nil {
		return "", "", 0, "", err
	}
	// 读出帮手或更新器写的状态。
	st, hasStatus, err := readStatus(dir)
	// 没能读出帮手或更新器写的状态就停，避免带着残缺继续。
	if err != nil {
		return "", "", 0, "", err
	}
	// 已经有换包结果就按成败来报。
	if hasStatus {
		// 旧 updater 不写版本，只能沿用待切换说明。
		kind, version = meta.Kind, meta.Version
		// 结果写明了换的是哪一版，就以它为准。
		if st.Version > 0 {
			// 待切换说明可能已换成下一版，只认 updater 自己记的。
			kind, version = st.Kind, st.Version
		}
		// 探活通过才把这次换成报成功。
		if st.OK {
			return "ok", kind, version, "", nil
		}
		return "fail", kind, version, st.Error, nil
	}
	// 还有待切换的包就报正在更换。
	if hasPending {
		return "applying", meta.Kind, meta.Version, "", nil
	}
	return "idle", "", 0, "", nil
}

// 读待切换说明；没有文件不算错。
func readPending(dir string) (pendingMeta, bool, error) {
	// 拼出同一目录下的路径。
	raw, err := os.ReadFile(filepath.Join(dir, pendingFile))
	// 待切换清单没读好或没写好就停，避免往下用残缺结果。
	if err != nil {
		// 文件还没有就按未就绪处理，不当成故障。
		if os.IsNotExist(err) {
			return pendingMeta{}, false, nil
		}
		return pendingMeta{}, false, err
	}
	// 准备承接待切换清单。
	var meta pendingMeta
	// 没能把字节还原成结构就停，避免带着残缺继续。
	if err := json.Unmarshal(raw, &meta); err != nil {
		return pendingMeta{}, false, err
	}
	return meta, true, nil
}

// 读 updater 结果；没有文件不算错。
func readStatus(dir string) (statusMeta, bool, error) {
	// 拼出同一目录下的路径。
	raw, err := os.ReadFile(filepath.Join(dir, statusFile))
	// 状态文件没读好或没写好就停，避免往下用残缺结果。
	if err != nil {
		// 文件还没有就按未就绪处理，不当成故障。
		if os.IsNotExist(err) {
			return statusMeta{}, false, nil
		}
		return statusMeta{}, false, err
	}
	// 准备承接状态文件里的结果。
	var st statusMeta
	// 没能把字节还原成结构就停，避免带着残缺继续。
	if err := json.Unmarshal(raw, &st); err != nil {
		return statusMeta{}, false, err
	}
	return st, true, nil
}

// 有待切换说明和包、却还没有结果，就是 updater 在排队或正在换。
func applyingNow(dir string) (bool, error) {
	// 说明和包缺一个，updater 都不会开工。
	for _, name := range []string{pendingFile, tarFile} {
		// 看这个文件在不在。
		ok, err := exists(filepath.Join(dir, name))
		// 查不了就交回错误，不在就不算在换。
		if err != nil || !ok {
			return false, err
		}
	}
	// 再看 updater 写没写结果。
	done, err := exists(filepath.Join(dir, statusFile))
	// 结果查不了就交回错误，不贸然覆盖。
	if err != nil {
		return false, err
	}
	return !done, nil
}

// 看文件在不在；不在不算错。
func exists(path string) (bool, error) {
	// 只取文件信息，不读正文。
	_, err := os.Stat(path)
	// 不在就交回否，不当成故障。
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// 删掉文件；本来就没有不算错。
func removeIfAny(path string) error {
	// 删不掉且不是本来就没有，才交回错误。
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// 清理镜像的结果和收回的空间。
type pruneMeta struct {
	OK        bool   `json:"ok"`                  // 清完
	Reclaimed string `json:"reclaimed,omitempty"` // docker 回报的空间
	Error     string `json:"error,omitempty"`     // 失败原因
}

// RequestPrune 写下清镜像请求，updater 来做；ref 空则清全部可清。
func (d Dir) RequestPrune(ref string) error {
	// 收成普通文本，方便当路径或名字用。
	dir := string(d)
	// 目录还空着就改用本机默认的更新位置。
	if dir == "" {
		// 没给目录时用本机默认的更新位置。
		dir = "/var/lib/wmesh/update"
	}
	// 没能先把目录准备出来就停，避免带着残缺继续。
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// 拼出同一目录下的路径。
	_ = os.Remove(filepath.Join(dir, pruneFile))
	// 没点名时按全部可清来写请求。
	body := "ALL"
	if ref != "" {
		// 点了具体目标就只清这一份。
		body = ref
	}
	// 拼出同一目录下的路径。
	tmp := filepath.Join(dir, pruneReq+".tmp")
	// 清理请求没读好或没写好就停，避免往下用残缺结果。
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	// 换名成功才算写完，读的人不会看见半截。
	return os.Rename(tmp, filepath.Join(dir, pruneReq))
}

// PruneResult 读清镜像结果；还没有结果则 present=false。
func (d Dir) PruneResult() (reclaimed string, ok bool, present bool, err error) {
	// 收成普通文本，方便当路径或名字用。
	dir := string(d)
	// 目录还空着就改用本机默认的更新位置。
	if dir == "" {
		// 没给目录时用本机默认的更新位置。
		dir = "/var/lib/wmesh/update"
	}
	// 拼出同一目录下的路径。
	raw, err := os.ReadFile(filepath.Join(dir, pruneFile))
	// 清理结果没读好或没写好就停，避免往下用残缺结果。
	if err != nil {
		// 文件还没有就按未就绪处理，不当成故障。
		if os.IsNotExist(err) {
			return "", false, false, nil
		}
		return "", false, false, err
	}
	// 准备承接清理镜像的结果。
	var st pruneMeta
	// 没能把字节还原成结构就停，避免带着残缺继续。
	if err := json.Unmarshal(raw, &st); err != nil {
		return "", false, true, err
	}
	return st.Reclaimed, st.OK, true, nil
}

// ImagesJSON 读 updater 写下的镜像占用；还没有文件则空。
func (d Dir) ImagesJSON() ([]byte, error) {
	// 收成普通文本，方便当路径或名字用。
	dir := string(d)
	// 目录还空着就改用本机默认的更新位置。
	if dir == "" {
		// 没给目录时用本机默认的更新位置。
		dir = "/var/lib/wmesh/update"
	}
	// 拼出同一目录下的路径。
	raw, err := os.ReadFile(filepath.Join(dir, imagesFile))
	// 镜像占用没读好或没写好就停，避免往下用残缺结果。
	if err != nil {
		// 文件还没有就按未就绪处理，不当成故障。
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return raw, nil
}
