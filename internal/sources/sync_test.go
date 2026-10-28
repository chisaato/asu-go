package sources

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chisaato/asu-go/internal/db"
)

func TestSyncHonorsEveryOverrideAndReplacesTargets(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	if err := store.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := list[0]
	patch := map[string]json.RawMessage{
		"branch": json.RawMessage(`"custom"`), "channel": json.RawMessage(`"custom"`),
		"enabled": json.RawMessage(`false`), "rolling": json.RawMessage(`true`),
		"upstream_url":    json.RawMessage(`"https://mirror.example"`),
		"package_manager": json.RawMessage(`"opkg"`),
		"image":           json.RawMessage(`"custom/image"`), "tag_template": json.RawMessage(`"fixed"`),
	}
	// 与默认 opkg 不同时才会标记 package_manager override。
	if before.PackageManager == "opkg" {
		patch["package_manager"] = json.RawMessage(`"apk"`)
	}
	before, err = store.Patch(ctx, before.ID, patch)
	if err != nil {
		t.Fatal(err)
	}
	incoming := before
	incoming.Branch, incoming.Channel, incoming.UpstreamURL = "upstream", "stable", "https://upstream.example"
	incoming.Enabled, incoming.Rolling = true, false
	incoming.Image, incoming.TagTemplate, incoming.PackageManager = "upstream/image", "{target}-{version_tag}", "opkg"
	incoming.Targets = map[string]any{"new": "target"}
	now := time.Now().UTC()
	incoming.SyncedAt = &now
	if err := store.SyncUpstream(ctx, incoming); err != nil {
		t.Fatal(err)
	}
	after, err := store.Get(ctx, before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Branch != before.Branch || after.Channel != before.Channel || after.Enabled != before.Enabled || after.Rolling != before.Rolling || after.UpstreamURL != before.UpstreamURL || after.PackageManager != before.PackageManager || after.Image != before.Image || after.TagTemplate != before.TagTemplate {
		t.Fatalf("覆盖了本地字段: %+v", after)
	}
	if after.Targets["new"] != "target" || len(after.Targets) != 1 || after.SyncedAt == nil {
		t.Fatalf("未替换 targets/时间: %+v", after)
	}
	incoming.Targets = map[string]any{"replacement": "only"}
	if err := store.SyncUpstream(ctx, incoming); err != nil {
		t.Fatal(err)
	}
	after, err = store.Get(ctx, before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Targets) != 1 || after.Targets["replacement"] != "only" {
		t.Fatalf("targets 被合并而非整块替换: %+v", after.Targets)
	}
}
