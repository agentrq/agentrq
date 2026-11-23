// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"fmt"

	handlerapi "github.com/agentrq/agentrq/backend/internal/handler/api"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// loginIconS3Namespace is the key prefix sign-in providers' icons get in the
// bucket.
const loginIconS3Namespace = "login-icons"

// newLoginIconStorage keeps sign-in providers' icons public in the bucket
// under login-icons when the s3 section names an endpoint and a bucket, as
// sign-in pictures are, and under localDir, read publicly at urlBase,
// otherwise.
func newLoginIconStorage(c config.Service, localDir, urlBase string) (storage.Service, error) {
	cfg, ok, err := bucketConfig(c)
	if err != nil {
		return nil, err
	}
	if !ok {
		return storage.NewNestedPublic(localDir, urlBase)
	}
	client, err := newS3(cfg)
	if err != nil {
		return nil, fmt.Errorf("s3: %w", err)
	}
	return storage.NewS3Public(client, loginIconS3Namespace), nil
}

// newOAuth2Logins signs in with each provider the operator added, which come
// back to <baseURL>/api/v1/auth/oauth2/<id>/callback.
func newOAuth2Logins(baseURL string, providers []auth.OAuth2Provider) []handlerapi.OAuth2Login {
	out := make([]handlerapi.OAuth2Login, len(providers))
	for i, p := range providers {
		callback := fmt.Sprintf("%s/api/v1/auth/oauth2/%s/callback", baseURL, p.ID)
		out[i] = handlerapi.OAuth2Login{ID: p.ID, Name: p.Name, IconURL: p.IconURL, Service: auth.NewOAuth2(p, callback)}
	}
	return out
}
