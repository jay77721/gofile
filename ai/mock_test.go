package ai

import (
	"context"
	"testing"
)

var ctx = context.Background()

func TestMockProvider_Analyze(t *testing.T) {
	p := NewMockProvider(128)
	ctx := context.Background()

	a, err := p.Analyze(ctx, "report.pdf", "这是季度报告的内容，包含预算分析...")
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}
	if a == nil {
		t.Fatal("Analyze returned nil")
	}
	// summary 包含文件名
	if a.Summary == "" {
		t.Error("summary should not be empty")
	}
	// pdf 应带"文档"标签
	foundDoc := false
	for _, tag := range a.Tags {
		if tag == "文档" {
			foundDoc = true
		}
	}
	if !foundDoc {
		t.Errorf("pdf should have 文档 tag, got %v", a.Tags)
	}
}

func TestMockProvider_Analyze_EmptyContent(t *testing.T) {
	p := NewMockProvider(64)
	a, err := p.Analyze(ctx, "photo.png", "")
	if err != nil {
		t.Fatal(err)
	}
	// 空内容时 summary 应为文件名
	if a.Summary != "photo.png" {
		t.Errorf("empty content summary should be filename, got %q", a.Summary)
	}
	foundImage := false
	for _, tag := range a.Tags {
		if tag == "图片" {
			foundImage = true
		}
	}
	if !foundImage {
		t.Errorf("png should have 图片 tag, got %v", a.Tags)
	}
}

func TestMockProvider_Analyze_LongContent(t *testing.T) {
	p := NewMockProvider(128)
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'a'
	}
	a, err := p.Analyze(ctx, "big.txt", string(long))
	if err != nil {
		t.Fatal(err)
	}
	// summary 应截断到 200 字符
	if len(a.Summary) > 250 {
		t.Errorf("summary should be truncated, got len %d", len(a.Summary))
	}
}

func TestMockProvider_BuildTags_ByExtension(t *testing.T) {
	cases := map[string]string{
		"a.pdf":  "文档",
		"a.docx": "文档",
		"a.xlsx": "表格",
		"a.csv":  "表格",
		"a.pptx": "演示",
		"a.png":  "图片",
		"a.jpg":  "图片",
		"a.mp4":  "视频",
		"a.mp3":  "音频",
		"a.zip":  "压缩包",
		"a.go":   "代码",
		"a.py":   "代码",
		"a.txt":  "文本",
		"a.md":   "文本",
		"a.json": "文本",
	}
	for name, want := range cases {
		tags := buildTags(name, "")
		found := false
		for _, tag := range tags {
			if tag == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: expected tag %q, got %v", name, want, tags)
		}
	}
}

func TestMockProvider_BuildTags_Keyword(t *testing.T) {
	tags := buildTags("年度预算报告.xlsx", "")
	foundBudget := false
	foundReport := false
	for _, tag := range tags {
		if tag == "预算" {
			foundBudget = true
		}
		if tag == "报告" {
			foundReport = true
		}
	}
	if !foundBudget || !foundReport {
		t.Errorf("expected 预算+报告 tags from filename keywords, got %v", tags)
	}
}

func TestMockProvider_Embed_Deterministic(t *testing.T) {
	p := NewMockProvider(128)
	v1, err := p.Embed(ctx, "hello world")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := p.Embed(ctx, "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if len(v1) != 128 || len(v2) != 128 {
		t.Errorf("embed dim should be 128, got %d/%d", len(v1), len(v2))
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("embed not deterministic at dim %d: %f vs %f", i, v1[i], v2[i])
			break
		}
	}
}

func TestMockProvider_Embed_DifferentText(t *testing.T) {
	p := NewMockProvider(64)
	v1, _ := p.Embed(ctx, "text one")
	v2, _ := p.Embed(ctx, "text two")
	same := true
	for i := range v1 {
		if v1[i] != v2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different texts should produce different vectors")
	}
}

func TestMockProvider_Dimension(t *testing.T) {
	p := NewMockProvider(256)
	if p.Dimension() != 256 {
		t.Errorf("Dimension should be 256, got %d", p.Dimension())
	}
	// 默认值
	p2 := NewMockProvider(0)
	if p2.Dimension() != 128 {
		t.Errorf("default Dimension should be 128, got %d", p2.Dimension())
	}
}
