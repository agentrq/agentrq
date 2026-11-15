// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package appconfig

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/security"
)

const testKey = "0123456789abcdef0123456789abcdef"

type dbConn struct{ db *gorm.DB }

func (c dbConn) Conn(context.Context) *gorm.DB { return c.db }
func (c dbConn) Close(context.Context)         {}

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&model.AppConfiguration{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func testBase(t *testing.T, yaml string) config.Service {
	t.Helper()
	t.Setenv("ENV", "development")
	svc, err := config.NewFS(fstest.MapFS{
		"_config/base.yaml":        {Data: []byte(yaml)},
		"_config/development.yaml": {Data: []byte("")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

const baseYAML = `
auth:
  jwtSecret: jwt
  rootAccessToken: root
  workspaceTokenKey: key
smtp:
  host: mail
  port: 587
memory:
  maxFileSize: 1048576
postgres:
  dsn: host=db
sqlite:
  dsn: ./x.db
idgen:
  node: 3
`

func newTestController(t *testing.T, db *gorm.DB, base config.Service, check func(config.Service) error) Controller {
	t.Helper()
	if check == nil {
		check = func(config.Service) error { return nil }
	}
	c, err := New(Params{DB: dbConn{db}, Base: base, Key: testKey, Check: check})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func plainRows(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()
	var rows []model.AppConfiguration
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range rows {
		plain, err := security.Decrypt(r.Val, testKey, r.Nonce)
		if err != nil {
			t.Fatalf("%s: %v", r.ConfigKey, err)
		}
		out[r.ConfigKey] = plain
	}
	return out
}

func TestNewRefusesAKeyAESCannotUse(t *testing.T) {
	if _, err := New(Params{Key: "short"}); err == nil || !strings.Contains(err.Error(), "must be 32 bytes, not 5") {
		t.Fatalf("New with a short key: %v", err)
	}
}

func TestTheTableName(t *testing.T) {
	if got := (model.AppConfiguration{}).TableName(); got != "_app_configurations" {
		t.Fatalf("TableName = %q", got)
	}
}

// The first start stores every block from yaml and env vars, encrypted, apart
// from the database's own blocks, idgen and the two auth secrets.
func TestLoadSeedsTheBlocksFromYAML(t *testing.T) {
	db := testDB(t)
	c := newTestController(t, db, testBase(t, baseYAML), nil)
	svc, err := c.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	rows := plainRows(t, db)
	want := map[string]string{
		"auth":   `{"jwtSecret":"jwt"}`,
		"memory": `{"maxFileSize":1048576}`,
		"smtp":   `{"host":"mail","port":587}`,
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v", rows)
	}
	for k, v := range want {
		if rows[k] != v {
			t.Errorf("%s = %s, want %s", k, rows[k], v)
		}
	}
	var raw []model.AppConfiguration
	db.Find(&raw)
	for _, r := range raw {
		if strings.Contains(r.Val, "mail") || len(r.Nonce) != 24 {
			t.Errorf("%s is not encrypted: %+v", r.ConfigKey, r)
		}
	}

	var auth struct {
		JWTSecret         string `yaml:"jwtSecret"`
		RootAccessToken   string `yaml:"rootAccessToken"`
		WorkspaceTokenKey string `yaml:"workspaceTokenKey"`
	}
	if err := svc.Populate("auth", &auth); err != nil || auth.JWTSecret != "jwt" || auth.RootAccessToken != "root" || auth.WorkspaceTokenKey != "key" {
		t.Fatalf("auth = %+v, %v", auth, err)
	}
}

// After the first start, a stored block wins over yaml and env vars, and a
// setting it lacks keeps the yaml's value.
func TestLoadPrefersTheStoredBlocks(t *testing.T) {
	db := testDB(t)
	first := newTestController(t, db, testBase(t, baseYAML), nil)
	if _, err := first.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Update(context.Background(), entity.UpdateAppConfigurationRequest{Key: "smtp", Value: json.RawMessage(`{"port":2525}`)}); err != nil {
		t.Fatal(err)
	}

	changed := strings.Replace(baseYAML, "host: mail", "host: other\n  from: me@x", 1)
	svc, err := newTestController(t, db, testBase(t, changed), nil).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var smtp struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
		From string `yaml:"from"`
	}
	if err := svc.Populate("smtp", &smtp); err != nil || smtp.Host != "other" || smtp.Port != 2525 || smtp.From != "me@x" {
		t.Fatalf("smtp = %+v, %v", smtp, err)
	}
}

// A row carrying an env-only field, written by hand, never overrides the env.
func TestLoadIgnoresStoredEnvOnlyFields(t *testing.T) {
	db := testDB(t)
	c := newTestController(t, db, testBase(t, baseYAML), nil).(*controller)
	row := c.row("auth", map[string]any{"jwtSecret": "x", "rootAccessToken": "hijack"})
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	svc, err := c.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var auth struct {
		JWTSecret       string `yaml:"jwtSecret"`
		RootAccessToken string `yaml:"rootAccessToken"`
	}
	if err := svc.Populate("auth", &auth); err != nil || auth.JWTSecret != "x" || auth.RootAccessToken != "root" {
		t.Fatalf("auth = %+v, %v", auth, err)
	}
}

func TestLoadErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("a row another key encrypted", func(t *testing.T) {
		db := testDB(t)
		val, nonce, _ := security.Encrypt(`{}`, "fedcba9876543210fedcba9876543210")
		db.Create(&model.AppConfiguration{ConfigKey: "smtp", Val: val, Nonce: nonce})
		_, err := newTestController(t, db, testBase(t, baseYAML), nil).Load(ctx)
		if err == nil || !strings.Contains(err.Error(), "config smtp: cannot decrypt it with auth.workspaceTokenKey") {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("a row that is not an object", func(t *testing.T) {
		db := testDB(t)
		val, nonce, _ := security.Encrypt(`[1]`, testKey)
		db.Create(&model.AppConfiguration{ConfigKey: "smtp", Val: val, Nonce: nonce})
		_, err := newTestController(t, db, testBase(t, baseYAML), nil).Load(ctx)
		if err == nil || !strings.HasPrefix(err.Error(), "config smtp: ") {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("no table", func(t *testing.T) {
		db := testDB(t)
		db.Migrator().DropTable(&model.AppConfiguration{})
		_, err := newTestController(t, db, testBase(t, baseYAML), nil).Load(ctx)
		if err == nil || !strings.HasPrefix(err.Error(), "read config: ") {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("a block that cannot be read", func(t *testing.T) {
		db := testDB(t)
		// Whichever block the overlay reads first is the one named.
		_, err := newTestController(t, db, failingConfig{testBase(t, baseYAML)}, nil).Load(ctx)
		if err == nil || !strings.HasSuffix(err.Error(), ": unreadable") {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("a seed the database refuses", func(t *testing.T) {
		db := testDB(t)
		db.Callback().Create().Before("gorm:create").Register("refuse", func(tx *gorm.DB) { tx.AddError(errors.New("disk full")) })
		_, err := newTestController(t, db, testBase(t, baseYAML), nil).Load(ctx)
		if err == nil || err.Error() != "seed config auth: disk full" {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("the read back after seeding", func(t *testing.T) {
		db := testDB(t)
		reads := 0
		db.Callback().Query().Before("gorm:query").Register("second", func(tx *gorm.DB) {
			if reads++; reads == 2 {
				tx.AddError(errors.New("connection lost"))
			}
		})
		_, err := newTestController(t, db, testBase(t, baseYAML), nil).Load(ctx)
		if err == nil || err.Error() != "read config: connection lost" {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("a stored block the overlay cannot lay", func(t *testing.T) {
		db := testDB(t)
		c := newTestController(t, db, testBase(t, baseYAML), nil)
		if _, err := c.Load(ctx); err != nil {
			t.Fatal(err)
		}
		// Whichever block the overlay reads first is the one named.
		_, err := newTestController(t, db, failingConfig{testBase(t, baseYAML)}, nil).Load(ctx)
		if err == nil || !strings.HasSuffix(err.Error(), ": unreadable") {
			t.Fatalf("Load: %v", err)
		}
	})
}

// failingConfig fails every Populate.
type failingConfig struct{ config.Service }

func (failingConfig) Populate(string, any) error { return errors.New("unreadable") }

func TestListAndUpdate(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	var checked config.Service
	c := newTestController(t, db, testBase(t, baseYAML), func(svc config.Service) error {
		checked = svc
		var mem struct {
			MaxFileSize int `yaml:"maxFileSize"`
		}
		if err := svc.Populate("memory", &mem); err != nil {
			return err
		}
		if mem.MaxFileSize < 0 {
			return errors.New("memory.maxFileSize: negative")
		}
		return nil
	})
	if _, err := c.Load(ctx); err != nil {
		t.Fatal(err)
	}

	list, err := c.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, b := range list {
		keys = append(keys, b.Key+"="+string(b.Value))
	}
	if got := strings.Join(keys, " "); got != `auth={"jwtSecret":"••••••••"} memory={"maxFileSize":1048576} smtp={"host":"mail","port":587}` {
		t.Fatalf("List = %s", got)
	}

	got, err := c.Update(ctx, entity.UpdateAppConfigurationRequest{Key: "auth", Value: json.RawMessage(`{"jwtSecret":"new","workspaceTokenKey":"stolen","n":12345678901234567890}`)})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"jwtSecret":"••••••••","n":12345678901234567890}`; string(got.Value) != want {
		t.Fatalf("Update = %s, want %s", got.Value, want)
	}
	if rows := plainRows(t, db); rows["auth"] != `{"jwtSecret":"new","n":12345678901234567890}` {
		t.Fatalf("stored auth = %s", rows["auth"])
	}
	var auth struct {
		JWTSecret string `yaml:"jwtSecret"`
	}
	if checked.Populate("auth", &auth); auth.JWTSecret != "new" {
		t.Fatalf("Check saw auth = %+v", auth)
	}

	if _, err := c.Update(ctx, entity.UpdateAppConfigurationRequest{Key: "memory", Value: json.RawMessage(`{"maxFileSize":-1}`)}); !isRefused(err, "memory.maxFileSize: negative") {
		t.Fatalf("Update with a value Check refuses: %v", err)
	}
	if rows := plainRows(t, db); rows["memory"] != `{"maxFileSize":1048576}` {
		t.Fatalf("a refused block was stored: %s", rows["memory"])
	}
	for _, v := range []string{`[1]`, `"x"`, `null`, `{`} {
		if _, err := c.Update(ctx, entity.UpdateAppConfigurationRequest{Key: "smtp", Value: json.RawMessage(v)}); !isRefused(err, "smtp: the value must be a JSON object") {
			t.Errorf("Update with %s: %v", v, err)
		}
	}
	for _, key := range []string{"postgres", "sqlite", "idgen", "nope"} {
		if _, err := c.Update(ctx, entity.UpdateAppConfigurationRequest{Key: key, Value: json.RawMessage(`{}`)}); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("Update %s: %v", key, err)
		}
	}
}

// A secret never leaves the server, and a save cannot overwrite one by
// mistake: the mask, or leaving it out, keeps it; only a new value replaces
// it, and an empty one clears it. Nested blocks alike.
func TestSecretsAreMaskedAndKept(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	yaml := `
smtp:
  host: mail
  password: pw$%#!
  unsetToken: ""
s3:
  bucket: b
  creds:
    secretAccessKey: sk
    accessKey: ak
`
	c := newTestController(t, db, testBase(t, yaml), nil)
	if _, err := c.Load(ctx); err != nil {
		t.Fatal(err)
	}
	list, _ := c.List(ctx)
	shown := map[string]string{}
	for _, b := range list {
		shown[b.Key] = string(b.Value)
	}
	if shown["smtp"] != `{"host":"mail","password":"••••••••","unsetToken":""}` || shown["s3"] != `{"bucket":"b","creds":{"accessKey":"ak","secretAccessKey":"••••••••"}}` {
		t.Fatalf("List = %v", shown)
	}

	update := func(key, value string) string {
		t.Helper()
		got, err := c.Update(ctx, entity.UpdateAppConfigurationRequest{Key: key, Value: json.RawMessage(value)})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(got.Value), "pw$") || strings.Contains(string(got.Value), "new-pw") {
			t.Fatalf("Update sent a secret back: %s", got.Value)
		}
		return plainRows(t, db)[key]
	}
	// Saved as shown: the mask keeps the secret.
	if got := update("smtp", `{"host":"mail2","password":"••••••••","unsetToken":""}`); got != `{"host":"mail2","password":"pw$%#!","unsetToken":""}` {
		t.Fatalf("mask: %s", got)
	}
	// Left out: kept too.
	if got := update("smtp", `{"host":"mail3"}`); got != `{"host":"mail3","password":"pw$%#!","unsetToken":""}` {
		t.Fatalf("left out: %s", got)
	}
	// A mask with nothing behind it is unset, never stored as the mask.
	if got := update("smtp", `{"host":"mail3","otherToken":"••••••••"}`); got != `{"host":"mail3","otherToken":"","password":"pw$%#!","unsetToken":""}` {
		t.Fatalf("mask over nothing: %s", got)
	}
	// A new value replaces it; an empty one clears it.
	if got := update("smtp", `{"password":"new-pw"}`); got != `{"otherToken":"","password":"new-pw","unsetToken":""}` {
		t.Fatalf("new value: %s", got)
	}
	if got := update("smtp", `{"password":""}`); got != `{"otherToken":"","password":"","unsetToken":""}` {
		t.Fatalf("cleared: %s", got)
	}
	// Nested: kept when its group is sent masked, or left out.
	if got := update("s3", `{"bucket":"b2","creds":{"accessKey":"ak2","secretAccessKey":"••••••••"}}`); got != `{"bucket":"b2","creds":{"accessKey":"ak2","secretAccessKey":"sk"}}` {
		t.Fatalf("nested mask: %s", got)
	}
	if got := update("s3", `{"bucket":"b3"}`); got != `{"bucket":"b3","creds":{"secretAccessKey":"sk"}}` {
		t.Fatalf("nested left out: %s", got)
	}
	// A group sent as something else is the operator's to replace.
	if got := update("s3", `{"creds":"none"}`); got != `{"creds":"none"}` {
		t.Fatalf("group replaced: %s", got)
	}
}

func isRefused(err error, msg string) bool {
	var refused *entity.AppConfigurationError
	return errors.As(err, &refused) && refused.Message == msg
}

func TestUpdateErrors(t *testing.T) {
	ctx := context.Background()
	rq := entity.UpdateAppConfigurationRequest{Key: "smtp", Value: json.RawMessage(`{"port":1}`)}

	t.Run("no table", func(t *testing.T) {
		db := testDB(t)
		c := newTestController(t, db, testBase(t, baseYAML), nil)
		db.Migrator().DropTable(&model.AppConfiguration{})
		if _, err := c.Update(ctx, rq); err == nil || !strings.HasPrefix(err.Error(), "read config: ") {
			t.Fatalf("Update: %v", err)
		}
	})

	t.Run("a base the overlay cannot read", func(t *testing.T) {
		db := testDB(t)
		c := newTestController(t, db, failingConfig{testBase(t, baseYAML)}, nil)
		if _, err := c.Update(ctx, rq); !isRefused(err, "smtp: unreadable") {
			t.Fatalf("Update: %v", err)
		}
	})

	t.Run("a write the database refuses", func(t *testing.T) {
		db := testDB(t)
		c := newTestController(t, db, testBase(t, baseYAML), nil)
		db.Callback().Create().Before("gorm:create").Register("refuse", func(tx *gorm.DB) { tx.AddError(errors.New("disk full")) })
		if _, err := c.Update(ctx, rq); err == nil || err.Error() != "disk full" {
			t.Fatalf("Update: %v", err)
		}
	})

	t.Run("List with no table", func(t *testing.T) {
		db := testDB(t)
		c := newTestController(t, db, testBase(t, baseYAML), nil)
		db.Migrator().DropTable(&model.AppConfiguration{})
		if _, err := c.List(ctx); err == nil {
			t.Fatal("List with no table succeeded")
		}
	})
}
