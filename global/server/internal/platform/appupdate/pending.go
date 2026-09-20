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

type pendingMeta struct {
	Kind    string `json:"kind"`    // wan_service
	Version int64  `json:"version"` // 待切换版本
	Digest  []byte `json:"digest"`  // SHA-256
}

type statusMeta struct {
	OK    bool   `json:"ok"`              // 探活通过
	Error string `json:"error,omitempty"` // 失败原因，不进业务库
}

// Report 读 updater 状态；没有 status 则 present=false。
func (d Dir) Report() (kind string, version int64, ok bool, present bool, err error) {
	dir := string(d)
	if dir == "" {
		dir = "/var/lib/wmesh/update"
	}
	raw, err := os.ReadFile(filepath.Join(dir, statusFile))
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, false, false, nil
		}
		return "", 0, false, false, err
	}
	var st statusMeta
	if err := json.Unmarshal(raw, &st); err != nil {
		return "", 0, false, true, err
	}
	metaRaw, err := os.ReadFile(filepath.Join(dir, pendingFile))
	if err != nil {
		return "", 0, st.OK, true, err
	}
	var meta pendingMeta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return "", 0, st.OK, true, err
	}
	return meta.Kind, meta.Version, st.OK, true, nil
}

// Stage 只写 tar 和待切换说明，不换容器。
func (d Dir) Stage(kind string, version int64, digest, body []byte) error {
	dir := string(d)
	if dir == "" {
		dir = "/var/lib/wmesh/update"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(dir, statusFile))
	tmp := filepath.Join(dir, tarFile+".tmp")
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, tarFile)); err != nil {
		return err
	}
	raw, err := json.Marshal(pendingMeta{Kind: kind, Version: version, Digest: digest})
	if err != nil {
		return err
	}
	metaTmp := filepath.Join(dir, pendingFile+".tmp")
	if err := os.WriteFile(metaTmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(metaTmp, filepath.Join(dir, pendingFile))
}

// Progress 读待切换与 updater 结果：idle / applying / ok / fail。
func (d Dir) Progress() (phase, kind string, version int64, errMsg string, err error) {
	dir := string(d)
	if dir == "" {
		dir = "/var/lib/wmesh/update"
	}
	meta, hasPending, err := readPending(dir)
	if err != nil {
		return "", "", 0, "", err
	}
	st, hasStatus, err := readStatus(dir)
	if err != nil {
		return "", "", 0, "", err
	}
	if hasStatus {
		if st.OK {
			return "ok", meta.Kind, meta.Version, "", nil
		}
		return "fail", meta.Kind, meta.Version, st.Error, nil
	}
	if hasPending {
		return "applying", meta.Kind, meta.Version, "", nil
	}
	return "idle", "", 0, "", nil
}

// 读待切换说明；没有文件不算错。
func readPending(dir string) (pendingMeta, bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, pendingFile))
	if err != nil {
		if os.IsNotExist(err) {
			return pendingMeta{}, false, nil
		}
		return pendingMeta{}, false, err
	}
	var meta pendingMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return pendingMeta{}, false, err
	}
	return meta, true, nil
}

// 读 updater 结果；没有文件不算错。
func readStatus(dir string) (statusMeta, bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, statusFile))
	if err != nil {
		if os.IsNotExist(err) {
			return statusMeta{}, false, nil
		}
		return statusMeta{}, false, err
	}
	var st statusMeta
	if err := json.Unmarshal(raw, &st); err != nil {
		return statusMeta{}, false, err
	}
	return st, true, nil
}

type pruneMeta struct {
	OK        bool   `json:"ok"`                  // 清完
	Reclaimed string `json:"reclaimed,omitempty"` // docker 回报的空间
	Error     string `json:"error,omitempty"`     // 失败原因
}

// RequestPrune 写下清镜像请求，updater 来做；ref 空则清全部可清。
func (d Dir) RequestPrune(ref string) error {
	dir := string(d)
	if dir == "" {
		dir = "/var/lib/wmesh/update"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(dir, pruneFile))
	body := "ALL"
	if ref != "" {
		body = ref
	}
	tmp := filepath.Join(dir, pruneReq+".tmp")
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, pruneReq))
}

// PruneResult 读清镜像结果；还没有结果则 present=false。
func (d Dir) PruneResult() (reclaimed string, ok bool, present bool, err error) {
	dir := string(d)
	if dir == "" {
		dir = "/var/lib/wmesh/update"
	}
	raw, err := os.ReadFile(filepath.Join(dir, pruneFile))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, false, nil
		}
		return "", false, false, err
	}
	var st pruneMeta
	if err := json.Unmarshal(raw, &st); err != nil {
		return "", false, true, err
	}
	return st.Reclaimed, st.OK, true, nil
}

// ImagesJSON 读 updater 写下的镜像占用；还没有文件则空。
func (d Dir) ImagesJSON() ([]byte, error) {
	dir := string(d)
	if dir == "" {
		dir = "/var/lib/wmesh/update"
	}
	raw, err := os.ReadFile(filepath.Join(dir, imagesFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return raw, nil
}
