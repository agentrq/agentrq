package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSwarmAndTaskSwarmFieldsMigrate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	if err := db.AutoMigrate(&Swarm{}, &Task{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	s := Swarm{ID: 1, Name: "test-swarm", WorkspaceID: 10, LeaderWorkspaceID: 100, MemberWorkspaceIDs: []byte(`[100,200]`)}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("create swarm: %v", err)
	}

	tsk := Task{ID: 2, WorkspaceID: 100, IsSwarmEnabled: true, SwarmID: 1}
	if err := db.Create(&tsk).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	var got Task
	if err := db.First(&got, 2).Error; err != nil {
		t.Fatalf("get task: %v", err)
	}
	if !got.IsSwarmEnabled || got.SwarmID != 1 {
		t.Errorf("expected IsSwarmEnabled=true SwarmID=1, got IsSwarmEnabled=%v SwarmID=%d", got.IsSwarmEnabled, got.SwarmID)
	}
}
