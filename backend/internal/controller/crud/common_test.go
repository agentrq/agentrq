package crud

import (
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_image "github.com/agentrq/agentrq/backend/internal/service/mocks/image"
	mock_notif "github.com/agentrq/agentrq/backend/internal/service/mocks/notif"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	mock_storage "github.com/agentrq/agentrq/backend/internal/service/mocks/storage"
	mock_telemetry "github.com/agentrq/agentrq/backend/internal/service/mocks/telemetry"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
)

type testEnv struct {
	controller Controller
	repo       *mock_repo.MockRepository
	idgen      *mock_idgen.MockService
	storage    *mock_storage.MockService
	image      *mock_image.MockService
	notif      *mock_notif.MockService
	telemetry  *mock_telemetry.MockService
}

func newTestController(t *testing.T) *testEnv {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := mock_repo.NewMockRepository(ctrl)
	idgen := mock_idgen.NewMockService(ctrl)
	stor := mock_storage.NewMockService(ctrl)
	img := mock_image.NewMockService(ctrl)
	notifSvc := mock_notif.NewMockService(ctrl)
	telSvc := mock_telemetry.NewMockService(ctrl)

	c := New(Params{
		IDGen:      idgen,
		Repository: repo,
		Storage:    stor,
		Image:      img,
		Notif:      notifSvc,
		Telemetry:  telSvc,
		TokenKey:   "test-key",
	})
	return &testEnv{
		controller: c,
		repo:       repo,
		idgen:      idgen,
		storage:    stor,
		image:      img,
		notif:      notifSvc,
		telemetry:  telSvc,
	}
}

const (
	testUserIDStr = "12345"
)

var testUserID = monoflake.IDFromBase62(testUserIDStr).Int64()

func activeWorkspace() model.Workspace {
	return model.Workspace{ID: 1, UserID: testUserID, Name: "ws"}
}
