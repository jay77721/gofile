package ai

import (
	"strings"
	"testing"
)

func TestParseQuery_TimeRange(t *testing.T) {
	cases := []struct {
		q       string
		wantNil bool
	}{
		{"上个月报告", false},
		{"这个月文件", false},
		{"上周", false},
		{"昨天", false},
		{"去年", false},
		{"最近3天", false},
		{"最近", false},
		{"找那个PDF", true}, // 无时间短语
		{"", true},
	}
	for _, c := range cases {
		f := ParseQuery(c.q)
		if (f.TimeRange == nil) != c.wantNil {
			t.Errorf("ParseQuery(%q): TimeRange nil=%v, want nil=%v", c.q, f.TimeRange == nil, c.wantNil)
		}
	}
}

func TestParseQuery_TypeFilter(t *testing.T) {
	cases := []struct {
		q    string
		want string
	}{
		{"图片", "tags:=[图片]"},
		{"照片", "tags:=[图片]"},
		{"png", "tags:=[图片]"},
		{"视频", "tags:=[视频]"},
		{"表格", "tags:=[表格]"},
		{"excel", "tags:=[表格]"},
		{"文档", "tags:=[文档]"},
		{"pdf", "tags:=[文档]"},
		{"代码", "tags:=[代码]"},
	}
	for _, c := range cases {
		f := ParseQuery(c.q)
		if f.TypeFilter != c.want {
			t.Errorf("ParseQuery(%q): TypeFilter=%q, want %q", c.q, f.TypeFilter, c.want)
		}
	}
}

func TestParseQuery_SemanticQuery(t *testing.T) {
	f := ParseQuery("找那个季度预算报告")
	// 去停用词后应保留"季度预算报告"
	if !strings.Contains(f.SemanticQuery, "季度") {
		t.Errorf("SemanticQuery should contain 季度, got %q", f.SemanticQuery)
	}
	if strings.Contains(f.SemanticQuery, "找") || strings.Contains(f.SemanticQuery, "那个") {
		t.Errorf("SemanticQuery should not contain stopwords, got %q", f.SemanticQuery)
	}
}

func TestParseQuery_Combined(t *testing.T) {
	f := ParseQuery("上个月预算的excel")
	if f.TimeRange == nil {
		t.Error("should detect 上个月")
	}
	if f.TypeFilter != "tags:=[表格]" {
		t.Errorf("should detect excel as 表格, got %q", f.TypeFilter)
	}
	if !strings.Contains(f.SemanticQuery, "预算") {
		t.Errorf("semantic should contain 预算, got %q", f.SemanticQuery)
	}
}

func TestParseQuery_RecentNDays(t *testing.T) {
	f := ParseQuery("最近3天")
	if f.TimeRange == nil || f.TimeRange.Start == 0 {
		t.Error("最近3天 should produce Start")
	}
	f2 := ParseQuery("最近")
	if f2.TimeRange == nil || f2.TimeRange.Start == 0 {
		t.Error("最近 should produce Start (default 7 days)")
	}
}
