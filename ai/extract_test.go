package ai

import (
	"strings"
	"testing"
)

func TestSampleText_Empty(t *testing.T) {
	if sampleText("", "x.txt") != "" {
		t.Error("empty text should stay empty")
	}
}

func TestSampleText_Short(t *testing.T) {
	s := "hello world"
	if sampleText(s, "x.txt") != s {
		t.Errorf("short text should be unchanged, got %q", sampleText(s, "x.txt"))
	}
}

func TestSampleText_Long(t *testing.T) {
	// 构造超长文本，含 markdown 标题与代码定义行作为锚点
	var sb strings.Builder
	sb.WriteString("开头内容\n")
	sb.WriteString("# 重要标题\n")
	sb.WriteString("func main() {}\n")
	for i := 0; i < 2000; i++ {
		sb.WriteString("填充内容行\n")
	}
	sb.WriteString("结尾内容\n")
	long := sb.String()

	got := sampleText(long, "x.txt")
	if len(got) > maxSummaryChars+100 {
		t.Errorf("sampled text too long: %d", len(got))
	}
	// 应保留锚点
	if !strings.Contains(got, "重要标题") {
		t.Error("should keep markdown heading anchor")
	}
	if !strings.Contains(got, "func main()") {
		t.Error("should keep code anchor")
	}
}

func TestIsAnchorLine(t *testing.T) {
	anchors := []string{"# 标题", "func main()", "class Foo", "import os", `"key": "value"`, "ERROR: failed"}
	for _, a := range anchors {
		if !isAnchorLine(a) {
			t.Errorf("expected %q to be an anchor", a)
		}
	}
	nonAnchors := []string{"普通文本", "123", "a = b + c"}
	for _, a := range nonAnchors {
		if isAnchorLine(a) {
			t.Errorf("expected %q NOT to be an anchor", a)
		}
	}
}

func TestStripXML(t *testing.T) {
	in := `<root><name>hello</name><value>world</value></root>`
	got := stripXML(in)
	if got != "helloworld" {
		t.Errorf("stripXML failed, got %q", got)
	}
}

func TestGetExt(t *testing.T) {
	cases := map[string]string{
		"a.pdf":    ".pdf",
		"a.tar.gz": ".gz",
		"noext":    "",
		".hidden":  ".hidden",
	}
	for in, want := range cases {
		if got := getExt(in); got != want {
			t.Errorf("getExt(%q)=%q, want %q", in, got, want)
		}
	}
}
