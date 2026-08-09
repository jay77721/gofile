package ai

import (
	"strings"
	"time"
)

// QueryFilter 对话式查询解析出的结构化过滤条件
type QueryFilter struct {
	TimeRange    *TimeRange // 时间范围（命中时间短语时为非 nil）
	TypeFilter   string     // Typesense filter_by 片段（如 tags:=[图片] 或 filename:*.pdf），空表示无类型约束
	SemanticQuery string     // 去除停用词后的语义查询词，传给全文+向量检索
}

// TimeRange 时间范围（Unix 毫秒，对应 Typesense created_at int64 字段）
type TimeRange struct {
	Start int64 // >= Start，0 表示不限制
	End   int64 // < End，0 表示不限制
}

// ParseQuery 将用户自然语言查询解析为结构化过滤 + 语义查询
//
// 规则实现（mock 阶段够用，真实 provider 阶段可升级为 LLM 解析）：
//  1. 时间短语 → created_at 范围
//  2. 类型词 → 扩展名/标签过滤
//  3. 剩余去停用词 → SemanticQuery
func ParseQuery(q string) *QueryFilter {
	f := &QueryFilter{}
	if q == "" {
		return f
	}
	s := q

	// 1. 时间短语
	if tr, ok := matchTimeRange(s); ok {
		f.TimeRange = tr
	}

	// 2. 类型词
	f.TypeFilter = matchTypeFilter(s)

	// 3. 去停用词，剩余作为语义查询
	f.SemanticQuery = stripStopwords(s)
	return f
}

// matchTimeRange 识别时间短语，返回 Unix 毫秒范围
func matchTimeRange(s string) (*TimeRange, bool) {
	now := time.Now()
	tr := &TimeRange{}

	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, "上个月"):
		firstOfThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		firstOfLastMonth := firstOfThisMonth.AddDate(0, -1, 0)
		tr.Start = firstOfLastMonth.UnixMilli()
		tr.End = firstOfThisMonth.UnixMilli()
		return tr, true
	case strings.Contains(lower, "这个月") || strings.Contains(lower, "本月"):
		firstOfThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		tr.Start = firstOfThisMonth.UnixMilli()
		return tr, true
	case strings.Contains(lower, "上上周"):
		tr.End = time.Date(now.Year(), now.Month(), now.Day()-7, 0, 0, 0, 0, now.Location()).UnixMilli()
		return tr, true
	case strings.Contains(lower, "上周"):
		// 上周一 ~ 本周一
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		thisMonday := time.Date(now.Year(), now.Month(), now.Day()-weekday+1, 0, 0, 0, 0, now.Location())
		lastMonday := thisMonday.AddDate(0, 0, -7)
		tr.Start = lastMonday.UnixMilli()
		tr.End = thisMonday.UnixMilli()
		return tr, true
	case strings.Contains(lower, "昨天"):
		yesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location())
		tr.Start = yesterday.UnixMilli()
		tr.End = yesterday.AddDate(0, 0, 1).UnixMilli()
		return tr, true
	case strings.Contains(lower, "今天"):
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		tr.Start = today.UnixMilli()
		return tr, true
	case strings.Contains(lower, "去年"):
		lastYear := time.Date(now.Year()-1, 1, 1, 0, 0, 0, 0, now.Location())
		tr.Start = lastYear.UnixMilli()
		tr.End = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location()).UnixMilli()
		return tr, true
	case strings.Contains(lower, "今年"):
		thisYear := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
		tr.Start = thisYear.UnixMilli()
		return tr, true
	case strings.Contains(lower, "最近"):
		if n, ok := parseNDays(lower); ok {
			tr.Start = now.AddDate(0, 0, -n).UnixMilli()
			return tr, true
		}
		// 默认最近 7 天
		tr.Start = now.AddDate(0, 0, -7).UnixMilli()
		return tr, true
	}
	return nil, false
}

// matchTypeFilter 识别类型词，返回 Typesense filter_by 片段
func matchTypeFilter(s string) string {
	lower := strings.ToLower(s)
	switch {
	case containsAny(lower, []string{"图片", "照片", "截图", "image", "photo", "screenshot", "png", "jpg", "jpeg", "gif", "webp"}):
		return "tags:=[图片]"
	case containsAny(lower, []string{"视频", "video", "mp4", "movie"}):
		return "tags:=[视频]"
	case containsAny(lower, []string{"音频", "音乐", "audio", "mp3"}):
		return "tags:=[音频]"
	case containsAny(lower, []string{"表格", "excel", "xlsx", "xls", "csv", "电子表格", "spreadsheet"}):
		return "tags:=[表格]"
	case containsAny(lower, []string{"文档", "pdf", "word", "docx", "doc"}):
		return "tags:=[文档]"
	case containsAny(lower, []string{"演示", "ppt", "pptx", "幻灯片", "slide"}):
		return "tags:=[演示]"
	case containsAny(lower, []string{"代码", "代码文件", "源码", "code", "source"}):
		return "tags:=[代码]"
	case containsAny(lower, []string{"压缩", "zip", "压缩包", "压缩文件", "archive"}):
		return "tags:=[压缩包]"
	}
	return ""
}

// stopwords 查询停用词（中文无空格分词，使用子串替换移除）
var stopwords = []string{
	"的", "了", "和", "与", "或", "在", "是", "有", "找", "一下", "一个", "那份",
	"那个", "这个", "什么", "哪些", "文件", "帮我", "请", "给", "我", "上个月",
	"这个月", "本月", "上周", "上上周", "昨天", "今天", "去年", "今年", "最近",
	"上", "下", "天", "周", "月", "年",
}

// stripStopwords 去掉查询中的停用词与类型词，保留语义关键词
func stripStopwords(s string) string {
	out := s
	// 子串式停用词移除（中文无空格分词，必须用子串替换）
	for _, sw := range stopwords {
		out = strings.ReplaceAll(out, sw, " ")
	}
	// 移除残留的类型/时间词（英文关键词同样用子串替换）
	for _, kw := range []string{"excel", "xlsx", "pdf", "图片", "照片", "文档", "表格", "视频", "音频", "代码", "压缩包", "文件"} {
		out = strings.ReplaceAll(out, kw, " ")
	}
	out = strings.Join(strings.Fields(out), " ")
	return out
}

func containsAny(lower string, kws []string) bool {
	for _, k := range kws {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// parseNDays 解析"最近 N 天"中的 N
func parseNDays(lower string) (int, bool) {
	// 支持 "最近3天" "最近 3 天" "最近三天"
	s := strings.ReplaceAll(lower, " ", "")
	if strings.HasPrefix(s, "最近") && strings.HasSuffix(s, "天") {
		num := strings.TrimSuffix(strings.TrimPrefix(s, "最近"), "天")
		if num == "" {
			return 0, false
		}
		// 中文数字
		if n, ok := chineseNum(num); ok {
			return n, true
		}
		// 阿拉伯数字
		var n int
		for _, c := range num {
			if c < '0' || c > '9' {
				return 0, false
			}
			n = n*10 + int(c-'0')
		}
		if n > 0 {
			return n, true
		}
	}
	return 0, false
}

func chineseNum(s string) (int, bool) {
	switch s {
	case "一":
		return 1, true
	case "二":
		return 2, true
	case "两":
		return 2, true
	case "三":
		return 3, true
	case "四":
		return 4, true
	case "五":
		return 5, true
	case "六":
		return 6, true
	case "七":
		return 7, true
	case "八":
		return 8, true
	case "九":
		return 9, true
	case "十":
		return 10, true
	}
	return 0, false
}
