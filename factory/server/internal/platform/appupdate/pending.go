// Package appupdate 把已确认的 app 包落到更新目录，供本机 updater 来换容器。
// 不管 Docker，也不做业务判定。
package appupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	tarFile     = "pending.tar"  // 确认后落下的镜像包
	pendingFile = "pending.json" // 种类、版本、摘要
	statusFile  = "status.json"  // updater 写就绪或失败
	pruneReq    = "prune.req"    // 请 updater 清无用镜像
	pruneFile   = "prune.json"   // updater 写清镜像结果
	imagesFile  = "images.json"  // updater 写本机 app 镜像占用
)

// Dir 把确认后的包写到 dir。
type Dir string

// 待切换的种类、版本和摘要。
type pendingMeta struct {
	Kind    string `json:"kind"`    // factory_service
	Version int64  `json:"version"` // 待切换版本
	Digest  []byte `json:"digest"`  // SHA-256
}

// 换包结果，成功还是失败原因。
type statusMeta struct {
	OK    bool   `json:"ok"`              // 探活通过
	Error string `json:"error,omitempty"` // 失败原因，不进业务库
}

// Report 读 updater 状态；没有 status 则 present=false。
func (d Dir) Report() (kind string, version int64, ok bool, present bool, err error) {
	// 收成普通文本，方便当路径或名字用。
	dir := string(d)
	// 目录还空着就改用本机默认的更新位置。
	if dir == "" {
		// 没给目录时用本机默认的更新位置。
		dir = "/var/lib/wmesh/update"
	}
	// 拼出同一目录下的路径。
	raw, err := os.ReadFile(filepath.Join(dir, statusFile))
	// 状态文件没读好或没写好就停，避免往下用残缺结果。
	if err != nil {
		// 文件还没有就按未就绪处理，不当成故障。
		if os.IsNotExist(err) {
			return "", 0, false, false, nil
		}
		return "", 0, false, false, err
	}
	// 准备承接状态文件里的结果。
	var st statusMeta
	// 没能把字节还原成结构就停，避免带着残缺继续。
	if err := json.Unmarshal(raw, &st); err != nil {
		return "", 0, false, true, err
	}
	// 拼出同一目录下的路径。
	metaRaw, err := os.ReadFile(filepath.Join(dir, pendingFile))
	// 待切换清单没读好或没写好就停，避免往下用残缺结果。
	if err != nil {
		return "", 0, st.OK, true, err
	}
	// 准备承接待切换清单。
	var meta pendingMeta
	// 没能把字节还原成结构就停，避免带着残缺继续。
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return "", 0, st.OK, true, err
	}
	return meta.Kind, meta.Version, st.OK, true, nil
}

// Stage 只写 tar 和待切换说明，不换容器。
func (d Dir) Stage(kind string, version int64, digest, body []byte) error {
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
	_ = os.Remove(filepath.Join(dir, statusFile))
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

// Progress 读待切换与 updater 结果：idle / applying / ok / fail。
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
		// 探活通过才把这次换成报成功。
		if st.OK {
			return "ok", meta.Kind, meta.Version, "", nil
		}
		return "fail", meta.Kind, meta.Version, st.Error, nil
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
