package ai

import (
	"strings"
	"time"
)

// QueryFilter 对话式查询解析出的结构化过滤条件
type QueryFilter struct {
	TimeRange     *TimeRange // 时间范围（命中时间短语时为非 nil）
	TypeFilter    string     // Typesense filter_by 片段（如 tags:=[图片]），空表示无类型约束
	SemanticQuery string     // 去除停用词后的语义查询词，传给全文+向量检索
}

// TimeRange 时间范围（Unix 毫秒，对应 Typesense created_at int64 字段）
type TimeRange struct {
	Start int64 // >= Start，0 表示不限制
	End   int64 // < End，0 表示不限制
}

// timePhrase 时间短语定义（按优先级排序，先匹配长的）
type timePhrase struct {
	keywords []string
	callback func(now time.Time) *TimeRange
}

var timePhrases = []timePhrase{
	{
		keywords: []string{"上个月"},
		callback: func(now time.Time) *TimeRange {
			firstOfThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
			firstOfLastMonth := firstOfThisMonth.AddDate(0, -1, 0)
			return &TimeRange{Start: firstOfLastMonth.UnixMilli(), End: firstOfThisMonth.UnixMilli()}
		},
	},
	{
		keywords: []string{"这个月", "本月"},
		callback: func(now time.Time) *TimeRange {
			firstOfThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
			return &TimeRange{Start: firstOfThisMonth.UnixMilli()}
		},
	},
	{
		keywords: []string{"上上周"},
		callback: func(now time.Time) *TimeRange {
			end := time.Date(now.Year(), now.Month(), now.Day()-7, 0, 0, 0, 0, now.Location())
			return &TimeRange{End: end.UnixMilli()}
		},
	},
	{
		keywords: []string{"上周"},
		callback: func(now time.Time) *TimeRange {
			weekday := int(now.Weekday())
			if weekday == 0 {
				weekday = 7
			}
			thisMonday := time.Date(now.Year(), now.Month(), now.Day()-weekday+1, 0, 0, 0, 0, now.Location())
			lastMonday := thisMonday.AddDate(0, 0, -7)
			return &TimeRange{Start: lastMonday.UnixMilli(), End: thisMonday.UnixMilli()}
		},
	},
	{
		keywords: []string{"昨天"},
		callback: func(now time.Time) *TimeRange {
			yesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location())
			return &TimeRange{Start: yesterday.UnixMilli(), End: yesterday.AddDate(0, 0, 1).UnixMilli()}
		},
	},
	{
		keywords: []string{"今天"},
		callback: func(now time.Time) *TimeRange {
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			return &TimeRange{Start: today.UnixMilli()}
		},
	},
	{
		keywords: []string{"去年"},
		callback: func(now time.Time) *TimeRange {
			lastYear := time.Date(now.Year()-1, 1, 1, 0, 0, 0, 0, now.Location())
			thisYear := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
			return &TimeRange{Start: lastYear.UnixMilli(), End: thisYear.UnixMilli()}
		},
	},
	{
		keywords: []string{"今年"},
		callback: func(now time.Time) *TimeRange {
			thisYear := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
			return &TimeRange{Start: thisYear.UnixMilli()}
		},
	},
	{
		keywords: []string{"最近"},
		callback: func(now time.Time) *TimeRange {
			return &TimeRange{Start: now.AddDate(0, 0, -7).UnixMilli()}
		},
	},
}

// typePhrase 类型短语定义
type typePhrase struct {
	keywords []string
	filter   string
}

var typePhrases = []typePhrase{
	{[]string{"图片", "照片", "截图", "image", "photo", "screenshot", "png", "jpg", "jpeg", "gif", "webp"}, "tags:=[图片]"},
	{[]string{"视频", "video", "mp4", "movie"}, "tags:=[视频]"},
	{[]string{"音频", "音乐", "audio", "mp3"}, "tags:=[音频]"},
	{[]string{"表格", "excel", "xlsx", "xls", "csv", "电子表格", "spreadsheet"}, "tags:=[表格]"},
	{[]string{"文档", "pdf", "word", "docx", "doc"}, "tags:=[文档]"},
	{[]string{"演示", "ppt", "pptx", "幻灯片", "slide"}, "tags:=[演示]"},
	{[]string{"代码", "代码文件", "源码", "code", "source"}, "tags:=[代码]"},
	{[]string{"压缩", "zip", "压缩包", "压缩文件", "archive"}, "tags:=[压缩包]"},
}

// ParseQuery 将用户自然语言查询解析为结构化过滤 + 语义查询
//
// 解析流程（逐层剥离，避免子串替换互相干扰）：
//  1. 提取时间短语 → created_at 范围，从文本中移除已匹配部分
//  2. 提取类型词 → tags filter，从文本中移除已匹配部分
//  3. 剩余文本去停用词 → SemanticQuery
func ParseQuery(q string) *QueryFilter {
	f := &QueryFilter{}
	if q == "" {
		return f
	}

	rest := q

	// 1. 提取时间短语
	if tr, matched, ok := matchTimeRange(rest); ok {
		f.TimeRange = tr
		rest = removeFirst(rest, matched)
	}

	// 2. 提取类型词
	if tf, matched := matchTypeFilter(rest); tf != "" {
		f.TypeFilter = tf
		rest = removeFirst(rest, matched)
	}

	// 3. 对剩余文本去停用词
	f.SemanticQuery = stripStopwords(rest)
	return f
}

// matchTimeRange 识别时间短语，返回范围及匹配到的关键词
func matchTimeRange(s string) (*TimeRange, string, bool) {
	now := time.Now()
	lower := strings.ToLower(s)

	// "最近 N 天" 优先解析：若先走 timePhrases，"最近"条目会抢先用默认 7 天命中，
	// 使 parseNDays 成为死代码，"最近3天" 等查询会错误返回 7 天范围
	if strings.Contains(lower, "最近") {
		if n, ok := parseNDays(lower); ok {
			return &TimeRange{Start: now.AddDate(0, 0, -n).UnixMilli()}, "最近", true
		}
	}

	for _, tp := range timePhrases {
		for _, kw := range tp.keywords {
			if strings.Contains(lower, kw) {
				return tp.callback(now), kw, true
			}
		}
	}

	return nil, "", false
}

// matchTypeFilter 识别类型词，返回 filter 及匹配到的关键词
func matchTypeFilter(s string) (string, string) {
	lower := strings.ToLower(s)
	for _, tp := range typePhrases {
		for _, kw := range tp.keywords {
			if strings.Contains(lower, kw) {
				return tp.filter, kw
			}
		}
	}
	return "", ""
}

// stopwords 查询停用词表
var stopwords = []string{
	"的", "了", "和", "与", "或", "在", "是", "有", "找", "一下", "一个", "那份",
	"那个", "这个", "什么", "哪些", "帮我", "请", "给", "我",
	"上", "下",
}

// stripStopwords 去掉查询中的停用词，保留语义关键词
func stripStopwords(s string) string {
	out := s
	for _, sw := range stopwords {
		out = strings.ReplaceAll(out, sw, " ")
	}
	out = strings.Join(strings.Fields(out), " ")
	return out
}

// removeFirst 从字符串中移除首次出现的子串
func removeFirst(s, sub string) string {
	if sub == "" {
		return s
	}
	return strings.Replace(s, sub, " ", 1)
}

// parseNDays 解析"最近 N 天"中的 N
func parseNDays(lower string) (int, bool) {
	s := strings.ReplaceAll(lower, " ", "")
	if strings.HasPrefix(s, "最近") && strings.HasSuffix(s, "天") {
		num := strings.TrimSuffix(strings.TrimPrefix(s, "最近"), "天")
		if num == "" {
			return 0, false
		}
		if n, ok := chineseNum(num); ok {
			return n, true
		}
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
	case "二", "两":
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
