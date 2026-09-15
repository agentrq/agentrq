// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

func TestModelsMigrateAndRoundTrip(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&model.Machine{}, &model.EnrolmentCode{}, &model.Session{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	now := time.Now().UTC()
	m := model.Machine{ID: 1, UserID: 7, Name: "build-box", Hostname: "bb01",
		OS: "linux", Arch: "arm64", Version: "0.7.0",
		TokenHash: HashSecret("tok"), Enabled: true, LastSeenAt: &now,
		InstanceID: "pod-a", ConnectedAt: &now,
		MemTotal: 16 << 30, MemAvailable: 4 << 30, CPUPercent: 37.4}
	if err := db.Create(&m).Error; err != nil {
		t.Fatalf("create machine: %v", err)
	}

	var got model.Machine
	if err := db.First(&got, 1).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.TokenHash != m.TokenHash || got.InstanceID != "pod-a" || got.CPUPercent != 37.4 {
		t.Errorf("round trip lost data: %+v", got)
	}

	// The unique index on the token hash is what stops two machines sharing a
	// credential.
	dup := model.Machine{ID: 2, UserID: 7, TokenHash: m.TokenHash}
	if err := db.Create(&dup).Error; err == nil {
		t.Error("two machines were allowed the same token hash")
	}

	s := model.Session{ID: 10, MachineID: 1, UserID: 7, Kind: "claude-code",
		Status: "running", Cols: 120, Rows: 40, StartedAt: &now}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	c := model.EnrolmentCode{ID: 20, UserID: 7, CodeHash: HashSecret("ABCD-2345"),
		ExpiresAt: now.Add(CodeTTL)}
	if err := db.Create(&c).Error; err != nil {
		t.Fatalf("create enrolment code: %v", err)
	}
}
