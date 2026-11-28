// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/mustafaturan/monoflake"
)

// accountSettingRule is the values one field may hold; the first is its
// default.
type accountSettingRule []string

// accountSettingGroups are the settings an account keeps: each group is a row
// holding its fields as a JSON object. A new group or field is an entry here,
// with no migration.
var accountSettingGroups = map[string]map[string]accountSettingRule{
	entity.SettingsDisplay: {
		entity.SettingTimeFormat: {entity.TimeFormatBrowser, entity.TimeFormat12h, entity.TimeFormat24h},
	},
}

// AccountSettingsController keeps the settings that hold in every workspace
// of an account.
type AccountSettingsController interface {
	GetAccountSettings(ctx context.Context, req entity.GetAccountSettingsRequest) (*entity.AccountSettingsResponse, error)
	UpdateAccountSettings(ctx context.Context, req entity.UpdateAccountSettingsRequest) (*entity.AccountSettingsResponse, error)
}

func (c *controller) GetAccountSettings(ctx context.Context, req entity.GetAccountSettingsRequest) (*entity.AccountSettingsResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid request")
	}
	rows, err := c.repository.ListAccountSettings(ctx, uid)
	if err != nil {
		return nil, err
	}
	return &entity.AccountSettingsResponse{Settings: accountSettingsOf(rows)}, nil
}

// UpdateAccountSettings saves the fields it is sent, each into its group's
// stored object, and returns every setting. An unknown group or field, or a
// value a field may not hold, refuses the whole save.
func (c *controller) UpdateAccountSettings(ctx context.Context, req entity.UpdateAccountSettingsRequest) (*entity.AccountSettingsResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid request")
	}
	// By name, so a save with two refusals always names the same one.
	groups := sortedKeys(req.Settings)
	for _, group := range groups {
		rules, ok := accountSettingGroups[group]
		if !ok {
			return nil, &entity.AccountSettingsError{Message: fmt.Sprintf("%q is not a group of account settings", group)}
		}
		for _, field := range sortedKeys(req.Settings[group]) {
			rule, ok := rules[field]
			if !ok {
				return nil, &entity.AccountSettingsError{Message: fmt.Sprintf("%q is not one of the %s", field, group)}
			}
			if value := req.Settings[group][field]; !slices.Contains(rule, value) {
				return nil, &entity.AccountSettingsError{Message: fmt.Sprintf("%s %q is not one of %s", field, value, oneOf(rule))}
			}
		}
	}
	rows, err := c.repository.ListAccountSettings(ctx, uid)
	if err != nil {
		return nil, err
	}
	saved := false
	for _, group := range groups {
		if len(req.Settings[group]) == 0 {
			continue
		}
		fields := storedFields(rows, group)
		for field, value := range req.Settings[group] {
			fields[field] = value
		}
		value, _ := json.Marshal(fields)
		row := model.AccountSetting{UserID: uid, Name: group, Value: string(value)}
		if err := c.repository.SaveAccountSetting(ctx, row); err != nil {
			return nil, err
		}
		rows = append(slices.DeleteFunc(rows, func(r model.AccountSetting) bool { return r.Name == group }), row)
		saved = true
	}
	if saved {
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       entity.ActionAccountSettingsUpdate,
			UserID:       uid,
			ResourceType: entity.ResourceUser,
			ResourceID:   uid,
			Actor:        entity.ActorHuman,
		})
	}
	return &entity.AccountSettingsResponse{Settings: accountSettingsOf(rows)}, nil
}

// storedFields is the group's stored object, or an empty one when the account
// has none or it is not an object of strings.
func storedFields(rows []model.AccountSetting, group string) map[string]string {
	fields := map[string]string{}
	for _, r := range rows {
		if r.Name == group && json.Unmarshal([]byte(r.Value), &fields) != nil {
			fields = map[string]string{}
		}
	}
	return fields
}

// accountSettingsOf is every field of every group with the account's value,
// or its default where the account has none, or one the field may no longer
// hold.
func accountSettingsOf(rows []model.AccountSetting) entity.AccountSettings {
	settings := make(entity.AccountSettings, len(accountSettingGroups))
	for group, rules := range accountSettingGroups {
		stored := storedFields(rows, group)
		fields := make(map[string]string, len(rules))
		for field, rule := range rules {
			fields[field] = rule[0]
			if v, ok := stored[field]; ok && slices.Contains(rule, v) {
				fields[field] = v
			}
		}
		settings[group] = fields
	}
	return settings
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// oneOf lists values as a sentence does: "a", "b" or "c".
func oneOf(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}
