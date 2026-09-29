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

// 待切换说明：种类、版本和摘要。
type pendingMeta struct {
	Kind    string `json:"kind"`    // wan_service
	Version int64  `json:"version"` // 待切换版本
	Digest  []byte `json:"digest"`  // SHA-256
}

// 换容器之后写下的就绪或失败。
type statusMeta struct {
	OK    bool   `json:"ok"`              // 探活通过
	Error string `json:"error,omitempty"` // 失败原因，不进业务库
}

// Report 读 updater 状态；没有 status 则 present=false。
func (d Dir) Report() (kind string, version int64, ok bool, present bool, err error) {
	// 先取出目录字符串。
	dir := string(d)
	// 没给目录就用默认更新区。
	if dir == "" {
		// 默认落在本机更新目录。
		dir = "/var/lib/wmesh/update"
	}
	// 先读换容器的结果。
	raw, err := os.ReadFile(filepath.Join(dir, statusFile))
	// 读失败要区分还没有结果。
	if err != nil {
		// 文件不在表示还没换完。
		if os.IsNotExist(err) {
			return "", 0, false, false, nil
		}
		return "", 0, false, false, err
	}
	// 准备接换容器写下的结果。
	var st statusMeta
	// 拆不开则结果在，但内容坏了。
	if err := json.Unmarshal(raw, &st); err != nil {
		return "", 0, false, true, err
	}
	// 再读待切换说明，补上种类和版本。
	metaRaw, err := os.ReadFile(filepath.Join(dir, pendingFile))
	// 说明读不到就只交回成败。
	if err != nil {
		return "", 0, st.OK, true, err
	}
	// 准备接待切换说明。
	var meta pendingMeta
	// 说明拆不开也只交回成败。
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return "", 0, st.OK, true, err
	}
	return meta.Kind, meta.Version, st.OK, true, nil
}

// Stage 只写 tar 和待切换说明，不换容器。
func (d Dir) Stage(kind string, version int64, digest, body []byte) error {
	// 先取出目录字符串。
	dir := string(d)
	// 没给目录就用默认更新区。
	if dir == "" {
		// 默认落在本机更新目录。
		dir = "/var/lib/wmesh/update"
	}
	// 目录必须在，包才写得进去。
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// 清掉上一轮结果，避免把旧成败当成这次。
	_ = os.Remove(filepath.Join(dir, statusFile))
	// 先写到临时文件，写完再换名。
	tmp := filepath.Join(dir, tarFile+".tmp")
	// 包写失败则不替换正式文件。
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	// 换名失败则正式包保持原样。
	if err := os.Rename(tmp, filepath.Join(dir, tarFile)); err != nil {
		return err
	}
	// 把种类、版本和摘要收成说明。
	raw, err := json.Marshal(pendingMeta{Kind: kind, Version: version, Digest: digest})
	// 收不成说明就不写半份。
	if err != nil {
		return err
	}
	// 说明也先写临时文件。
	metaTmp := filepath.Join(dir, pendingFile+".tmp")
	// 说明写失败则不替换正式说明。
	if err := os.WriteFile(metaTmp, raw, 0o600); err != nil {
		return err
	}
	// 换名成功，updater 才能看到这一份。
	return os.Rename(metaTmp, filepath.Join(dir, pendingFile))
}

// Progress 读待切换与 updater 结果：idle / applying / ok / fail。
func (d Dir) Progress() (phase, kind string, version int64, errMsg string, err error) {
	// 先取出目录字符串。
	dir := string(d)
	// 没给目录就用默认更新区。
	if dir == "" {
		// 默认落在本机更新目录。
		dir = "/var/lib/wmesh/update"
	}
	// 看有没有待切换说明。
	meta, hasPending, err := readPending(dir)
	// 说明坏了就没法判断阶段。
	if err != nil {
		return "", "", 0, "", err
	}
	// 再看换容器的结果。
	st, hasStatus, err := readStatus(dir)
	// 结果坏了同样停住。
	if err != nil {
		return "", "", 0, "", err
	}
	// 有结果就分成功和失败。
	if hasStatus {
		// 探活通过就算换完了。
		if st.OK {
			return "ok", meta.Kind, meta.Version, "", nil
		}
		return "fail", meta.Kind, meta.Version, st.Error, nil
	}
	// 只有说明、还没有结果，就是正在换。
	if hasPending {
		return "applying", meta.Kind, meta.Version, "", nil
	}
	return "idle", "", 0, "", nil
}

// 读待切换说明；没有文件不算错。
func readPending(dir string) (pendingMeta, bool, error) {
	// 读出待切换的说明。
	raw, err := os.ReadFile(filepath.Join(dir, pendingFile))
	// 读失败要区分文件还没有。
	if err != nil {
		// 没有文件不算错，表示还没确认。
		if os.IsNotExist(err) {
			return pendingMeta{}, false, nil
		}
		return pendingMeta{}, false, err
	}
	// 准备接待切换说明的正文。
	var meta pendingMeta
	// 拆不开则这份说明不能用。
	if err := json.Unmarshal(raw, &meta); err != nil {
		return pendingMeta{}, false, err
	}
	return meta, true, nil
}

// 读 updater 结果；没有文件不算错。
func readStatus(dir string) (statusMeta, bool, error) {
	// 读出换容器写下的结果。
	raw, err := os.ReadFile(filepath.Join(dir, statusFile))
	// 读失败要区分结果还没有。
	if err != nil {
		// 没有文件不算错，表示还没写结果。
		if os.IsNotExist(err) {
			return statusMeta{}, false, nil
		}
		return statusMeta{}, false, err
	}
	// 准备接换容器写下的结果。
	var st statusMeta
	// 拆不开则这份结果不能用。
	if err := json.Unmarshal(raw, &st); err != nil {
		return statusMeta{}, false, err
	}
	return st, true, nil
}

// 清镜像之后写下的空间和成败。
type pruneMeta struct {
	OK        bool   `json:"ok"`                  // 清完
	Reclaimed string `json:"reclaimed,omitempty"` // docker 回报的空间
	Error     string `json:"error,omitempty"`     // 失败原因
}

// RequestPrune 写下清镜像请求，updater 来做；ref 空则清全部可清。
func (d Dir) RequestPrune(ref string) error {
	// 先取出目录字符串。
	dir := string(d)
	// 没给目录就用默认更新区。
	if dir == "" {
		// 默认落在本机更新目录。
		dir = "/var/lib/wmesh/update"
	}
	// 目录必须在，请求才写得进去。
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// 清掉上一轮结果，避免把旧空间当成这次。
	_ = os.Remove(filepath.Join(dir, pruneFile))
	// 没指定就请清掉全部可清镜像。
	body := "ALL"
	// 指定了就只清这一份。
	if ref != "" {
		// 请求正文改成这一份引用。
		body = ref
	}
	// 先写临时文件，写完再换名。
	tmp := filepath.Join(dir, pruneReq+".tmp")
	// 写失败则不替换正式请求。
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	// 换名成功，updater 才能看到请求。
	return os.Rename(tmp, filepath.Join(dir, pruneReq))
}

// PruneResult 读清镜像结果；还没有结果则 present=false。
func (d Dir) PruneResult() (reclaimed string, ok bool, present bool, err error) {
	// 先取出目录字符串。
	dir := string(d)
	// 没给目录就用默认更新区。
	if dir == "" {
		// 默认落在本机更新目录。
		dir = "/var/lib/wmesh/update"
	}
	// 读出清镜像写下的结果。
	raw, err := os.ReadFile(filepath.Join(dir, pruneFile))
	// 读失败要区分结果还没有。
	if err != nil {
		// 没有文件表示还没清完。
		if os.IsNotExist(err) {
			return "", false, false, nil
		}
		return "", false, false, err
	}
	// 准备接清镜像结果。
	var st pruneMeta
	// 拆不开则结果在，但内容坏了。
	if err := json.Unmarshal(raw, &st); err != nil {
		return "", false, true, err
	}
	return st.Reclaimed, st.OK, true, nil
}

// ImagesJSON 读 updater 写下的镜像占用；还没有文件则空。
func (d Dir) ImagesJSON() ([]byte, error) {
	// 先取出目录字符串。
	dir := string(d)
	// 没给目录就用默认更新区。
	if dir == "" {
		// 默认落在本机更新目录。
		dir = "/var/lib/wmesh/update"
	}
	// 读出本机镜像占用正文。
	raw, err := os.ReadFile(filepath.Join(dir, imagesFile))
	// 读失败要区分文件还没有。
	if err != nil {
		// 没有文件就交回空，不当错误。
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return raw, nil
}
