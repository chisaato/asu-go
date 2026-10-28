package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/chisaato/asu-go/internal/runtime"
	"go.uber.org/zap"
)

func TestImageWorkingDirAndOwnership(t *testing.T) {
	for _, tc := range []struct {
		dir, user string
		uid, gid  int
	}{
		{"/builder/", "buildbot", 1000, 1000},
		{"/home/build/immortalwrt/", "build", 1000, 1000},
		{"/custom/", "2000:3000", 2000, 3000},
		{"", "", 0, 0},
		{"/", "1001", 1001, 1001},
	} {
		t.Run(tc.dir+tc.user, func(t *testing.T) {
			dir := workingDir(tc.dir)
			r, observed, j := setupRunner(t, runtime.FakePlan{Archives: map[string][]byte{path.Join(dir, "job"): tarData(t, "job/firmware.bin", "ok", tar.TypeReg)}})
			fake := observed.Runtime.(*runtime.Fake)
			fake.Images = map[string]runtime.ImageMetadata{j.Image: {WorkingDir: tc.dir, User: tc.user}}
			j.Defaults = "#!/bin/sh\necho test\n"
			j.Repositories = map[string]string{"custom": "https://example.com/repo"}
			if _, err := r.Run(context.Background(), j); err != nil {
				t.Fatal(err)
			}
			if observed.spec.WorkingDir != dir || observed.spec.User != "" || observed.putPath != dir || observed.getPath != path.Join(dir, j.ID) {
				t.Fatal(observed.spec, observed.putPath, observed.getPath)
			}
			for _, want := range []string{"FILES=" + path.Join(dir, "asu-files"), "BIN_DIR=" + path.Join(dir, j.ID), path.Join(dir, j.ID, "manifest")} {
				if !strings.Contains(observed.spec.Command[2], want) {
					t.Fatal("脚本路径错误", want, observed.spec.Command[2])
				}
			}
			tr := tar.NewReader(bytes.NewReader(observed.input))
			entries, dirs := 0, 0
			for {
				hdr, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if hdr.Uid != tc.uid || hdr.Gid != tc.gid || path.IsAbs(hdr.Name) {
					t.Fatal(hdr)
				}
				entries++
				if hdr.Typeflag == tar.TypeDir {
					dirs++
				}
			}
			if entries == 0 || dirs == 0 {
				t.Fatal("缺少输入文件或目录")
			}
			// 重启接管使用容器实际目录，不被同名镜像后来变更影响。
			h, err := fake.Create(context.Background(), runtime.RunSpec{Image: j.Image, WorkingDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			if err := fake.Start(context.Background(), h); err != nil {
				t.Fatal(err)
			}
			fake.Images[j.Image] = runtime.ImageMetadata{WorkingDir: "/changed"}
			restarted := &Runner{Runtime: observed, Hub: r.Hub}
			if _, err := restarted.Resume(context.Background(), j, h); err != nil {
				t.Fatal(err)
			}
			if observed.getPath != path.Join(dir, j.ID) {
				t.Fatal(observed.getPath)
			}
		})
	}
}

func TestArchiveOwner(t *testing.T) {
	for _, user := range []string{"build", "invalid:user", "-1", "1000:", "4294967296"} {
		uid, gid := archiveOwner(user, zap.NewNop())
		if uid != 1000 || gid != 1000 {
			t.Fatal(user, uid, gid)
		}
	}
}

type imageInspectRuntime struct {
	runtime.Runtime
	inspections int
}

func (r *imageInspectRuntime) InspectImage(ctx context.Context, image string) (runtime.ImageMetadata, error) {
	r.inspections++
	return r.Runtime.InspectImage(ctx, image)
}

func TestImageMetadataCache(t *testing.T) {
	fake := runtime.NewFake()
	fake.Images = map[string]runtime.ImageMetadata{"image": {WorkingDir: "/first"}}
	rt := &imageInspectRuntime{Runtime: fake}
	r := &Runner{Runtime: rt}
	j := Job{Image: "image", PullPolicy: "missing"}
	for i := 0; i < 2; i++ {
		metadata, err := r.prepare(context.Background(), j)
		if err != nil || metadata.WorkingDir != "/first" {
			t.Fatal(metadata, err)
		}
	}
	if rt.inspections != 1 {
		t.Fatal("未复用镜像元数据", rt.inspections)
	}
	fake.Images[j.Image] = runtime.ImageMetadata{WorkingDir: "/updated"}
	j.PullPolicy = "always"
	metadata, err := r.prepare(context.Background(), j)
	if err != nil || metadata.WorkingDir != "/updated" || rt.inspections != 2 {
		t.Fatal("拉取后未刷新元数据", metadata, rt.inspections, err)
	}
}
