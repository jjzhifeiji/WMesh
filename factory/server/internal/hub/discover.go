package hub

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// FactoryProbe 是局域网探活回给 Client 的本厂摘要，无钥、无账号、无正文。
type FactoryProbe struct {
	FactoryID  uuid.UUID  `json:"factoryId"`             // 本厂稳定身份
	Name       string     `json:"factoryName,omitempty"` // 本厂显示名
	Status     string     `json:"status"`                // 本厂治理状态：active / disabled / retired
	Belongs    bool       `json:"belongs"`               // 该设备号是否已钉在本厂未作废 Client
	ClientID   *uuid.UUID `json:"clientId,omitempty"`    // 钉死该号的 Client；不属于则为空
	ClientName string     `json:"clientName,omitempty"`  // 给人看的设备名
}

// Discover 列出本机已认领工厂；带设备号时判定是否本厂已钉设备。
func (h *Hub) Discover(ctx context.Context, serial string) ([]FactoryProbe, error) {
	// 先列出本机已认领的厂，列不出就拒绝探活。
	ids, err := h.ListSite(ctx)
	// 厂列表失败就交回，平板看不到可登录的厂。
	if err != nil {
		return nil, err
	}
	// 去掉空白，空号表示只问有哪些厂。
	serial = strings.TrimSpace(serial)
	// 按厂数预留结果，探活列表不会中途丢厂。
	out := make([]FactoryProbe, 0, len(ids))
	// 逐家判断这台设备有没有钉在本厂。
	for _, site := range ids {
		// 先装厂名和治理状态，绑定稍后再补。
		row := FactoryProbe{FactoryID: site.ID, Name: site.Name, Status: site.Status}
		// 没带设备号就只回厂摘要，不去对绑定。
		if serial == "" {
			// 没有识别号也把这家厂放进结果。
			out = append(out, row)
			continue
		}
		// 打开这家厂，打不开就只回厂名。
		svc, err := h.Service(ctx, site.ID)
		// 厂库打不开仍回厂名，不把这一家从探活里抹掉。
		if err != nil {
			// 打不开也保留厂摘要，平板还能看到这家。
			out = append(out, row)
			continue
		}
		// 只认未作废且已钉该号的 Client，不当全厂设备名录。
		cli, err := svc.Store().BoundClientByDeviceSerial(ctx, serial)
		// 对不上已钉设备就只回厂名，不当成属于这家。
		if err != nil {
			// 没钉上也保留厂摘要，绑定标记保持否。
			out = append(out, row)
			continue
		}
		// 拷一份身份再取址，避免循环变量被下一家盖掉。
		id := cli.ID
		// 标明设备已经钉在这家厂。
		row.Belongs = true
		// 带回钉死的设备身份，不属于则留空。
		row.ClientID = &id
		// 带回给人看的设备名。
		row.ClientName = cli.Name
		// 带上绑定结果放进探活列表。
		out = append(out, row)
	}
	return out, nil
}
