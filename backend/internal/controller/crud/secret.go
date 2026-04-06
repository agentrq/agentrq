package crud

import (
	"context"
	"fmt"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/security"
	"github.com/mustafaturan/monoflake"
)

func (c *controller) CreateSecret(ctx context.Context, req entity.CreateSecretRequest) (*entity.CreateSecretResponse, error) {
	if req.Key == "" {
		return nil, fmt.Errorf("key is required")
	}
	if req.Key != "GOOGLE_JULES_API_KEY" {
		return nil, fmt.Errorf("invalid secret key: only GOOGLE_JULES_API_KEY is allowed")
	}
	if req.Value == "" {
		return nil, fmt.Errorf("value is required")
	}

	// Check if secret with this key already exists
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	now := time.Now()

	m, err := c.repository.GetSecretByKey(ctx, uid, req.Key)
	if err == nil {
		// Update existing secret
		enc, nonce, err := security.Encrypt(req.Value, c.tokenKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt secret: %w", err)
		}
		m.ValueEncrypted = enc
		m.Nonce = nonce
		m.UpdatedAt = now
		if err := c.repository.CreateSecret(ctx, m); err != nil {
			return nil, fmt.Errorf("failed to update secret: %w", err)
		}
		return &entity.CreateSecretResponse{Key: req.Key}, nil
	}

	// Create new secret
	enc, nonce, err := security.Encrypt(req.Value, c.tokenKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt secret: %w", err)
	}

	m = model.Secret{
		ID:             c.idgen.NextID(),
		CreatedAt:      now,
		UpdatedAt:      now,
		UserID:         uid,
		Key:            req.Key,
		ValueEncrypted: enc,
		Nonce:          nonce,
	}

	if err := c.repository.CreateSecret(ctx, m); err != nil {
		return nil, fmt.Errorf("failed to create secret: %w", err)
	}

	return &entity.CreateSecretResponse{Key: req.Key}, nil
}

func (c *controller) ListSecrets(ctx context.Context, userID string) (*entity.ListSecretsResponse, error) {
	uid := monoflake.IDFromBase62(userID).Int64()
	ms, err := c.repository.ListSecrets(ctx, uid)
	if err != nil {
		return nil, err
	}

	secrets := make([]entity.Secret, len(ms))
	for i, m := range ms {
		secrets[i] = entity.Secret{
			ID:        m.ID,
			CreatedAt: m.CreatedAt,
			UpdatedAt: m.UpdatedAt,
			Key:       m.Key,
		}
	}
	return &entity.ListSecretsResponse{Secrets: secrets}, nil
}
