package repository

import (
	"context"
	"testing"
	"time"

	"gofile/internal/domain"
)

func TestShareRepoLifecycle(t *testing.T) {
	db := newTestDB(t)
	repo := NewShareRepository(db)

	// 创建分享
	s := &model.Share{
		ShareToken:   "tok123456",
		FileSha1:     testHash,
		UserName:     "alice",
		PasswordHash: "hash",
		ExpireAt:     time.Now().Add(24 * time.Hour),
	}
	if err := repo.CreateShare(context.Background(), s); err != nil {
		t.Fatalf("CreateShare failed: %v", err)
	}

	// 按令牌查询
	got, err := repo.GetShareByToken(context.Background(), "tok123456")
	if err != nil {
		t.Fatalf("GetShareByToken failed: %v", err)
	}
	if got.UserName != "alice" || got.FileSha1 != testHash || got.PasswordHash != "hash" {
		t.Fatalf("share mismatch: %+v", got)
	}

	// 列表只含自己的
	_ = repo.CreateShare(context.Background(), &model.Share{ShareToken: "tokbob1", FileSha1: testHash, UserName: "bob", ExpireAt: time.Now().Add(time.Hour)})
	list, err := repo.ListShares(context.Background(), "alice")
	if err != nil || len(list) != 1 {
		t.Fatalf("alice should have 1 share, got %d err=%v", len(list), err)
	}

	// 撤销:非归属者失败
	if ok, _ := repo.DeleteShare(context.Background(), "tok123456", "bob"); ok {
		t.Fatal("bob should not revoke alice's share")
	}
	if ok, err := repo.DeleteShare(context.Background(), "tok123456", "alice"); err != nil || !ok {
		t.Fatalf("alice revoke failed: ok=%v err=%v", ok, err)
	}
	if _, err := repo.GetShareByToken(context.Background(), "tok123456"); err == nil {
		t.Fatal("revoked share should be gone")
	}
}

func TestShareRepoDeleteExpired(t *testing.T) {
	db := newTestDB(t)
	repo := NewShareRepository(db)

	_ = repo.CreateShare(context.Background(), &model.Share{ShareToken: "expired1", FileSha1: testHash, UserName: "alice", ExpireAt: time.Now().Add(-time.Hour)})
	_ = repo.CreateShare(context.Background(), &model.Share{ShareToken: "active1", FileSha1: testHash, UserName: "alice", ExpireAt: time.Now().Add(time.Hour)})

	if err := repo.DeleteExpired(context.Background(), time.Now()); err != nil {
		t.Fatalf("DeleteExpired failed: %v", err)
	}
	if _, err := repo.GetShareByToken(context.Background(), "expired1"); err == nil {
		t.Fatal("expired share should be cleaned")
	}
	if _, err := repo.GetShareByToken(context.Background(), "active1"); err != nil {
		t.Fatal("active share should survive")
	}
}

func TestAIConfigRepoCRUD(t *testing.T) {
	db := newTestDB(t)
	repo := NewAIConfigRepository(db)

	// 不存在 → ErrRecordNotFound
	if _, err := repo.Get("alice"); err == nil {
		t.Fatal("expected not found for unconfigured user")
	}

	cfg := &model.AIConfig{
		Username:   "alice",
		BaseURL:    "https://8.8.8.8/v1",
		APIKeyEnc:  "encrypted",
		Model:      "gpt-4o",
		EmbedModel: "text-embedding-3-small",
	}
	if err := repo.Upsert(cfg); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	got, err := repo.Get("alice")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.BaseURL != cfg.BaseURL || got.APIKeyEnc != "encrypted" || got.Model != "gpt-4o" {
		t.Fatalf("config mismatch: %+v", got)
	}

	// Upsert 覆盖更新
	cfg2 := &model.AIConfig{Username: "alice", BaseURL: "https://8.8.8.9/v1", APIKeyEnc: "enc2", Model: "gpt-4o-mini"}
	if err := repo.Upsert(cfg2); err != nil {
		t.Fatalf("second Upsert failed: %v", err)
	}
	got2, _ := repo.Get("alice")
	if got2.BaseURL != "https://8.8.8.9/v1" || got2.Model != "gpt-4o-mini" || got2.APIKeyEnc != "enc2" {
		t.Fatalf("upsert should overwrite, got %+v", got2)
	}

	// Delete
	if err := repo.Delete("alice"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := repo.Get("alice"); err == nil {
		t.Fatal("config should be gone after delete")
	}
}
