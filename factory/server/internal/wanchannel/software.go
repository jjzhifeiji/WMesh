package wanchannel

import (
	"context"
	"net/url"
	"strconv"

	"github.com/google/uuid"
)

// SoftwareMeta 是 WAN 该种类当前最高版本，不含字节。
type SoftwareMeta struct {
	Kind        string `json:"kind"`        // factory_service / client_apk
	Version     int64  `json:"version"`     // 当前最高
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // SHA-256
}

// 按版本拉到的软件包，字节只在这一步出现。
type softwareOffer struct {
	Kind        string `json:"kind"`        // factory_service / client_apk
	Version     int64  `json:"version"`     // 单调整数
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // SHA-256
	Body        []byte `json:"body"`        // 包字节
}

// LatestSoftware 已认领厂会话看该种类当前最高版本，不含字节。
func LatestSoftware(ctx context.Context, wanHTTP string, factoryID uuid.UUID, priv []byte, kind string) (SoftwareMeta, error) {
	// 用厂钥签查询，没有签名平台会拒绝。
	p := newPuller(wanHTTP, factoryID, priv)
	// 预备装最高版，失败时不把零值当成真版本。
	var meta SoftwareMeta
	// 查不到最高版就交回，调用方不要当成本地已有。
	if err := p.get(ctx, "/v1/software/latest?kind="+url.QueryEscape(kind), &meta); err != nil {
		return SoftwareMeta{}, err
	}
	return meta, nil
}

// PullSoftwareBody 已认领厂会话按版本拉包字节。
func PullSoftwareBody(ctx context.Context, wanHTTP string, factoryID uuid.UUID, priv []byte, kind string, version int64) ([]byte, error) {
	// 用厂钥签拉包，对不上就拿不到字节。
	p := newPuller(wanHTTP, factoryID, priv)
	// 预备装包体，失败时不能把空字节当成功。
	var offer softwareOffer
	// 种类和版本都放进查询，避免拉错包。
	path := "/v1/channel/pull/software?kind=" + url.QueryEscape(kind) + "&version=" + strconv.FormatInt(version, 10)
	// 拉包失败就交回，调用方不要安装空内容。
	if err := p.get(ctx, path, &offer); err != nil {
		return nil, err
	}
	return offer.Body, nil
}
