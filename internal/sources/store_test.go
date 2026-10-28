package sources

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/chisaato/asu-go/internal/db"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewStore(database)
}

func TestDefaultsAndCRUD(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	if err := store.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	initial, err := store.List(ctx)
	if err != nil || len(initial) < 4 {
		t.Fatalf("默认目录: %v %v", initial, err)
	}
	for _, source := range initial {
		if len(source.Overrides) != 0 {
			t.Fatalf("默认值不应包含覆盖字段: %v", source)
		}
		if _, err := ResolveImage(source, "mediatek/filogic"); err != nil {
			t.Fatal(err)
		}
	}
	first := initial[0]
	patch := map[string]json.RawMessage{"image": json.RawMessage(`"example/imagebuilder"`), "enabled": json.RawMessage(`false`)}
	updated, err := store.Patch(ctx, first.ID, patch)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled || !slices.Contains(updated.Overrides, "image") || !slices.Contains(updated.Overrides, "enabled") {
		t.Fatalf("覆盖记录缺失: %+v", updated)
	}
	if err := store.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, first.ID)
	if err != nil || !reflect.DeepEqual(got, updated) {
		t.Fatalf("重启覆盖了修改: %+v %v", got, err)
	}
	if err := store.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, first.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("不应补回已删除目录: %v", err)
	}
	source := first
	source.Distribution = "custom"
	source.Origin = "local"
	created, err := store.Create(ctx, source)
	if err != nil || created.ID == 0 || !slices.Contains(created.Overrides, "distribution") {
		t.Fatalf("新增失败: %+v %v", created, err)
	}
	if _, err := store.Create(ctx, source); !errors.Is(err, ErrConflict) {
		t.Fatalf("重复来源未冲突: %v", err)
	}
	for _, patch := range []map[string]json.RawMessage{{"id": json.RawMessage(`2`)}, {"enabled": json.RawMessage(`null`)}, {"package_manager": json.RawMessage(`"bad"`)}} {
		if _, err := store.Patch(ctx, created.ID, patch); !errors.Is(err, ErrInvalid) {
			t.Fatalf("非法补丁未拒绝: %v", err)
		}
	}
}

func TestCatalogRoundTripAndAtomicImport(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	if err := store.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	original, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	original[0].SyncedAt = &now
	original[0].Targets = map[string]any{"mediatek/filogic": map[string]any{"arch": "aarch64_cortex-a53"}}
	if err := store.Import(ctx, original); err != nil {
		t.Fatal(err)
	}
	original, err = store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Export(&out, original); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCatalog(&out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, decoded) {
		t.Fatalf("YAML 往返不一致: %#v %#v", original, decoded)
	}
	other := testStore(t)
	if err := other.Import(ctx, decoded); err != nil {
		t.Fatal(err)
	}
	got, err := other.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		got[i].ID = original[i].ID
	}
	if !reflect.DeepEqual(original, got) {
		t.Fatal("跨数据库导入导出不一致")
	}
	decoded[0].Image = "local/imagebuilder"
	if err := store.Import(ctx, decoded); err != nil {
		t.Fatal(err)
	}
	got, err = store.List(ctx)
	if err != nil || !slices.Contains(got[0].Overrides, "image") {
		t.Fatalf("import 未记录覆盖: %v %v", got, err)
	}
	decoded[0].Image = "should/not-persist"
	decoded[1].PackageManager = "invalid"
	if err := store.Import(ctx, decoded); !errors.Is(err, ErrInvalid) {
		t.Fatalf("无效导入未拒绝: %v", err)
	}
	got, err = store.List(ctx)
	if err != nil || got[0].Image != "local/imagebuilder" {
		t.Fatalf("导入未回滚: %v %v", got, err)
	}
	for _, input := range []string{"- distribution: test\n  typo: true\n", "[]\n---\n[]\n"} {
		if _, err := DecodeCatalog(bytes.NewBufferString(input)); err == nil {
			t.Fatal("严格解码未拒绝错误目录")
		}
	}
}

func TestResolveImage(t *testing.T) {
	for _, tc := range []struct{ distribution, version, template, tag string }{
		{"openwrt", "25.12.6", "{target}-{version_tag}", "mediatek-filogic-v25.12.6"},
		{"openwrt", "25.12.0-rc1", "{target}-{version_tag}", "mediatek-filogic-v25.12.0-rc1"},
		{"openwrt", "SNAPSHOT", "{target}-{version_tag}", "mediatek-filogic-master"},
		{"openwrt", "24.10-SNAPSHOT", "{target}-{version_tag}", "mediatek-filogic-openwrt-24.10"},
		{"immortalwrt", "25.12.2", "{target}-{version_tag}", "mediatek-filogic-openwrt-25.12.2"},
		{"immortalwrt", "25.12.0-rc1", "{target}-{version_tag}", "mediatek-filogic-openwrt-25.12.0-rc1"},
		{"immortalwrt", "SNAPSHOT", "{target}-{version_tag}", "mediatek-filogic-master"},
		{"immortalwrt", "24.10-SNAPSHOT", "{target}-{version_tag}", "mediatek-filogic-openwrt-24.10"},
		{"custom", "1.2.3", "{target}-{version}", "mediatek-filogic-1.2.3"},
		{"immortalwrt", "1.2.3", "fixed-tag", "fixed-tag"},
	} {
		t.Run(tc.distribution+"/"+tc.version+"/"+tc.template, func(t *testing.T) {
			source := Source{Distribution: tc.distribution, Version: tc.version, Image: "registry.example/imagebuilder", TagTemplate: tc.template}
			got, err := ResolveImage(source, "mediatek/filogic")
			if err != nil || got != source.Image+":"+tc.tag {
				t.Fatalf("%q %v", got, err)
			}
		})
	}
	if _, err := ResolveImage(Source{Image: "test", Version: "SNAPSHOT", TagTemplate: "{unknown}"}, "x/y"); err == nil {
		t.Fatal("未知模板未拒绝")
	}
}
