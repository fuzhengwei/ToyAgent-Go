package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// 本文件是 Go 版对 Java Json 工具类使用方式的适配层：
// JSON 编解码一律基于标准库 encoding/json（规范要求不移植手写 Json 工具类），
// 这里只封装场景代码中反复出现的「去代码围栏 → 解析对象 → 按 key 取字符串」，
// 以及与 Java Double.toString 对齐的数值格式化，语义与 Java 各场景一致。

var (
	fenceStartRe = regexp.MustCompile("^```\\w*\\s*")
	fenceEndRe   = regexp.MustCompile("```\\s*$")
)

// StripFence 去掉模型输出首尾的 markdown 代码围栏。
func StripFence(s string) string {
	return strings.TrimSpace(fenceEndRe.ReplaceAllString(fenceStartRe.ReplaceAllString(s, ""), ""))
}

// TryParse 把模型输出解析为 JSON 对象；不是合法 JSON 对象时返回 nil。
// （对应 Java 各场景 tryParse 失败时返回原始字符串、调用方用
// instanceof Map 判断的语义 —— Go 里用 nil 表示「模型没按协议输出」。）
func TryParse(raw string) map[string]any {
	var v any
	if err := json.Unmarshal([]byte(StripFence(raw)), &v); err != nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

// Str 从解析后的对象按 key 取字符串，缺失返回空串（对应 Java Json.str）。
func Str(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// Write 把任意 JSON 值序列化为字符串（对应 Java Json.write）。
func Write(v any) string {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "null"
	}
	return strings.TrimRight(sb.String(), "\n")
}

// SafeChat 调用模型并把 panic 转为 error 返回
// （对应 Java 场景里 try { model.chat(...) } catch (Exception e) 的位置）。
func SafeChat(model ChatModel, messages []Message) (answer string, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("%v", r)
			}
			answer = ""
		}
	}()
	return model.Chat(messages), nil
}

// FormatDouble 对齐 Java String.valueOf(double) 的教学输出：
// 整数值补 ".0"（如 3+5 → "8.0"），非整数原样（如 8.5）。
func FormatDouble(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.ContainsAny(s, ".neNI") { // 'f' 格式不会出 e/N/Inf，防御性判断
		s += ".0"
	}
	return s
}

// FormatIntLikeLong 对齐 Java (long) 强转后的输出：整数部分直接字符串化
// （如 8.0 → "8"、8.5 → "8"），NaN 场景由调用方先行处理。
func FormatIntLikeLong(v float64) string {
	return strconv.FormatInt(int64(v), 10)
}
