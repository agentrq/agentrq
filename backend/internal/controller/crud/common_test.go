package crud

import (
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_image "github.com/agentrq/agentrq/backend/internal/service/mocks/image"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	mock_storage "github.com/agentrq/agentrq/backend/internal/service/mocks/storage"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
)

type testEnv struct {
	controller Controller
	repo       *mock_repo.MockRepository
	idgen      *mock_idgen.MockService
	storage    *mock_storage.MockService
	image      *mock_image.MockService
	pubsub     *mock_pubsub.MockService
}

func newTestController(t *testing.T) *testEnv {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := mock_repo.NewMockRepository(ctrl)
	idgen := mock_idgen.NewMockService(ctrl)
	stor := mock_storage.NewMockService(ctrl)
	img := mock_image.NewMockService(ctrl)
	psSvc := mock_pubsub.NewMockService(ctrl)

	// Default expectation for PubSub Publish since it's called on almost every write
	psSvc.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

	c := New(Params{
		IDGen:      idgen,
		Repository: repo,
		Storage:    stor,
		Image:      img,
		PubSub:     psSvc,
		TokenKey:   "test-key",
	})

	return &testEnv{
		controller: c,
		repo:       repo,
		idgen:      idgen,
		storage:    stor,
		image:      img,
		pubsub:     psSvc,
	}
}

const (
	testUserIDStr = "12345"
)

var testUserID = monoflake.IDFromBase62(testUserIDStr).Int64()

func activeWorkspace() model.Workspace {
	return model.Workspace{ID: 1, UserID: testUserID, Name: "ws"}
}
