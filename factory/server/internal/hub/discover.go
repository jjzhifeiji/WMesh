package hub

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// FactoryProbe 是局域网探活回给 Client 的本厂摘要，无钥、无账号、无正文。
type FactoryProbe struct {
	FactoryID  uuid.UUID  `json:"factoryId"`            // 本厂稳定身份
	Status     string     `json:"status"`               // 本厂治理状态：active / disabled / retired
	Belongs    bool       `json:"belongs"`              // 该设备号是否已钉在本厂未作废 Client
	ClientID   *uuid.UUID `json:"clientId,omitempty"`   // 钉死该号的 Client；不属于则为空
	ClientName string     `json:"clientName,omitempty"` // 给人看的设备名
}

// Discover 列出本机已认领工厂；带设备号时判定是否本厂已钉设备。
func (h *Hub) Discover(ctx context.Context, serial string) ([]FactoryProbe, error) {
	ids, err := h.ListSite(ctx)
	if err != nil {
		return nil, err
	}
	serial = strings.TrimSpace(serial)
	out := make([]FactoryProbe, 0, len(ids))
	for _, site := range ids {
		row := FactoryProbe{FactoryID: site.ID, Status: site.Status}
		if serial == "" {
			out = append(out, row)
			continue
		}
		svc, err := h.Service(ctx, site.ID)
		if err != nil {
			out = append(out, row)
			continue
		}
		// 只认未作废且已钉该号的 Client，不当全厂设备名录。
		cli, err := svc.Store().BoundClientByDeviceSerial(ctx, serial)
		if err != nil {
			out = append(out, row)
			continue
		}
		id := cli.ID
		row.Belongs = true
		row.ClientID = &id
		row.ClientName = cli.Name
		out = append(out, row)
	}
	return out, nil
}
