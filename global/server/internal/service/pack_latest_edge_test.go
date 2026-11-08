// D-102 边界：云端另存、改依赖按身份取当前版，已声明的停用工艺不挡路。
package service_test

import (
	"errors"
	"testing"

	"wmesh/global/internal/platform/domain"
	global "wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 建一份加密平台工艺和正文写着它的工程。
func encryptedLatestEnv(t *testing.T) latestEnv {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 工艺新建默认不可复制，就是加密工艺。
	e := newLatestEnv(t)
	// 另存要改写正文引用，换成正文里写着这条工艺的单层工程。
	proj, err := e.h.WAN.CreatePlatformProject(e.ctx, e.tok, "加密依赖工程", copyWeldBody(e.proc.ID), nil, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把建工程的错误打出来并停住。
		t.Fatal(err)
	}
	// 夹具换成这份工程。
	e.proj = proj
	return e
}

// 加密工艺升版后另存工程：新工程记当前修订。
func TestCopyProjectEncryptedDepFollowsLatest(t *testing.T) {
	// 工艺和工程。
	e := encryptedLatestEnv(t)
	// 工艺升一版。
	up := e.upgradeProc(t, `{"current":266}`)
	// 另存工程。
	cp, err := e.h.WAN.CopyPlatformProcess(e.ctx, e.tok, e.proj.ID, "副本")
	// 工艺升版不该挡住另存。
	if err != nil {
		// 把另存的错误打出来并停住。
		t.Fatalf("copy: %v", err)
	}
	// 另存是一次新声明，记当前修订。
	if len(cp.Deps) != 1 || cp.Deps[0].ID != e.proc.ID || cp.Deps[0].Revision != up.Revision {
		// 把实际依赖打出来并停住。
		t.Fatalf("deps %+v want r%d", cp.Deps, up.Revision)
	}
}

// 加密工艺已停用：另存工程按停用拒绝，和普通工艺同一口径。
func TestCopyProjectEncryptedDepDisabled(t *testing.T) {
	// 工艺和工程。
	e := encryptedLatestEnv(t)
	// 停用工艺。
	if _, err := e.h.WAN.DisablePlatformAsset(e.ctx, e.tok, e.proc.ID, e.proc.Revision); err != nil {
		// 把停用的错误打出来并停住。
		t.Fatal(err)
	}
	// 新工程不得新依赖停用工艺。
	if _, err := e.h.WAN.CopyPlatformProcess(e.ctx, e.tok, e.proj.ID, "副本"); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 放行或错因不对就停。
		t.Fatalf("copy with disabled: %v", err)
	}
}

// 工程已声明的工艺后来停用：改依赖留着它照常成，只拒新加停用工艺。
func TestSetPlatformProjectDepsKeepsDeclaredDisabled(t *testing.T) {
	// 工艺和工程。
	e := newLatestEnv(t)
	// 再建一条可用工艺，准备新加。
	other := copyPublishedProcess(t, e.ctx, e.h, e.tok, "另一条", []byte(`{"current":271}`), true)
	// 再建一条可用工艺，停用后试着新加。
	never := copyPublishedProcess(t, e.ctx, e.h, e.tok, "没声明过", []byte(`{"current":272}`), true)
	// 停用没声明过的那条。
	off, err := e.h.WAN.DisablePlatformAsset(e.ctx, e.tok, never.ID, never.Revision)
	// 停用失败就停。
	if err != nil {
		// 把停用的错误打出来并停住。
		t.Fatal(err)
	}
	// 停用已声明的那条。
	if _, err := e.h.WAN.DisablePlatformAsset(e.ctx, e.tok, e.proc.ID, e.proc.Revision); err != nil {
		// 把停用的错误打出来并停住。
		t.Fatal(err)
	}
	// 留着已声明的，再加一条可用的。
	next, err := e.h.WAN.SetPlatformProjectDeps(e.ctx, e.tok, e.proj.ID, e.proj.Revision, []global.AssetDep{
		// 已声明的那条原样带回。
		{ID: e.proc.ID, Revision: e.proc.Revision, Digest: e.proc.Digest},
		// 新加可用工艺。
		{ID: other.ID, Revision: other.Revision, Digest: other.Digest},
	})
	// 已声明的停用工艺不该挡住改依赖。
	if err != nil {
		// 把改依赖的错误打出来并停住。
		t.Fatalf("set deps: %v", err)
	}
	// 两条都在，已声明的那条不重写。
	if len(next.Deps) != 2 || next.Deps[0].ID != e.proc.ID || next.Deps[0].Revision != e.proc.Revision {
		// 把实际依赖打出来并停住。
		t.Fatalf("deps %+v", next.Deps)
	}
	// 新加停用工艺仍拒绝。
	if _, err := e.h.WAN.SetPlatformProjectDeps(e.ctx, e.tok, next.ID, next.Revision, []global.AssetDep{
		// 已声明的留着。
		next.Deps[0], next.Deps[1],
		// 没声明过的停用工艺。
		{ID: off.ID, Revision: off.Revision, Digest: off.Digest},
	}); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 放行或错因不对就停。
		t.Fatalf("add disabled: %v", err)
	}
}
