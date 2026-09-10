package audit

import (
	"time"

	"github.com/google/uuid"
)

type Row struct {
	ID           uuid.UUID  `gorm:"type:uuid;primaryKey"`
	ActorID      *uuid.UUID `gorm:"type:uuid"`
	ClaimedLogin *string
	FactoryID    *uuid.UUID `gorm:"type:uuid"`
	OrgUnitID    *uuid.UUID `gorm:"type:uuid"`
	OrgPath      []byte     `gorm:"type:jsonb"`
	Action       string     `gorm:"not null"`
	Target       string     `gorm:"not null"`
	Result       string     `gorm:"not null"`
	TimeSource   string     `gorm:"not null"`
	OccurredAt   time.Time  `gorm:"not null"`
}

func (Row) TableName() string { return "audit_events" }

func RowFrom(e Event) Row {
	if e.TimeSource == "" {
		e.TimeSource = Server
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	return Row{
		ID:           e.ID,
		ActorID:      e.ActorID,
		ClaimedLogin: e.ClaimedLogin,
		FactoryID:    e.FactoryID,
		OrgUnitID:    e.OrgUnitID,
		OrgPath:      e.OrgPath,
		Action:       e.Action,
		Target:       e.Target,
		Result:       e.Result,
		TimeSource:   e.TimeSource,
		OccurredAt:   e.OccurredAt,
	}
}
