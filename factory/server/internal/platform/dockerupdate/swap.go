package dockerupdate

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Swap 给帮手容器用：docker load、建新容器、等确认写完已装版本，再停旧起新。
func Swap(args []string) error {
	fs := flag.NewFlagSet("docker-swap", flag.ContinueOnError)
	sock := fs.String("sock", sockPath, "docker socket")
	dir := fs.String("dir", defaultDir, "shared update directory")
	oldID := fs.String("old", "", "container to replace")
	name := fs.String("name", "", "original container name")
	delay := fs.Duration("delay", defaultDelay, "wait after load before stopping the old container")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*oldID) == "" || strings.TrimSpace(*name) == "" {
		return fmt.Errorf("docker-swap requires -old -name")
	}
	api := newUnixClient(unixPath(*sock))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	return runHelper(ctx, api, *dir, *oldID, *name, *delay)
}

// runHelper load 镜像并建好新容器后才写就绪；随后再换容器。
func runHelper(ctx context.Context, api *client, dir, oldID, name string, delay time.Duration) error {
	fail := func(err error) error {
		_ = writeStatus(dir, helperStatus{Error: err.Error()})
		return err
	}
	me, err := api.inspect(ctx, oldID)
	if err != nil {
		return fail(err)
	}
	if err := rejectInfra(me); err != nil {
		return fail(err)
	}
	f, err := os.Open(filepath.Join(dir, tarFile))
	if err != nil {
		return fail(err)
	}
	refs, err := api.loadImages(ctx, f)
	_ = f.Close()
	if err != nil {
		return fail(err)
	}
	image, err := pickImage(refs, me.Config.Image, me.Config.Labels)
	if err != nil {
		return fail(err)
	}
	nextName := name + nextSuffix
	if err := api.removeIfExists(ctx, nextName); err != nil {
		return fail(err)
	}
	newID, err := api.create(ctx, nextName, cloneApp(me, image))
	if err != nil {
		return fail(err)
	}
	if err := writeStatus(dir, helperStatus{OK: true}); err != nil {
		return err
	}
	if delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return swapContainers(ctx, api, oldID, newID, name)
}

// swapContainers 停掉并删除旧 app，把新容器改回原名再启动。
func swapContainers(ctx context.Context, api *client, oldID, newID, name string) error {
	if err := api.stop(ctx, oldID); err != nil && !isNotFound(err) {
		return fmt.Errorf("stop old: %w", err)
	}
	if err := api.remove(ctx, oldID); err != nil && !isNotFound(err) {
		return fmt.Errorf("remove old: %w", err)
	}
	if err := api.rename(ctx, newID, name); err != nil {
		return fmt.Errorf("rename new: %w", err)
	}
	if err := api.start(ctx, newID); err != nil {
		return fmt.Errorf("start new: %w", err)
	}
	return nil
}
