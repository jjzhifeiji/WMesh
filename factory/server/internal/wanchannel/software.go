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

type softwareOffer struct {
	Kind        string `json:"kind"`        // factory_service / client_apk
	Version     int64  `json:"version"`     // 单调整数
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // SHA-256
	Body        []byte `json:"body"`        // 包字节
}

// LatestSoftware 已认领厂会话看该种类当前最高版本，不含字节。
func LatestSoftware(ctx context.Context, wanHTTP string, factoryID uuid.UUID, priv []byte, kind string) (SoftwareMeta, error) {
	p := newPuller(wanHTTP, factoryID, priv)
	var meta SoftwareMeta
	if err := p.get(ctx, "/v1/software/latest?kind="+url.QueryEscape(kind), &meta); err != nil {
		return SoftwareMeta{}, err
	}
	return meta, nil
}

// PullSoftwareBody 已认领厂会话按版本拉包字节。
func PullSoftwareBody(ctx context.Context, wanHTTP string, factoryID uuid.UUID, priv []byte, kind string, version int64) ([]byte, error) {
	p := newPuller(wanHTTP, factoryID, priv)
	var offer softwareOffer
	path := "/v1/channel/pull/software?kind=" + url.QueryEscape(kind) + "&version=" + strconv.FormatInt(version, 10)
	if err := p.get(ctx, path, &offer); err != nil {
		return nil, err
	}
	return offer.Body, nil
}
