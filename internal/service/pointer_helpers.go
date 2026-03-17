package service

// pointerInt 返回 int 指针，便于构造模型字段。
func pointerInt(v int) *int {
	return &v
}

// pointerInt64 返回 int64 指针，供各服务重复使用。
func pointerInt64(v int64) *int64 {
	return &v
}

// pointerString 返回字符串指针，统一字符串指针创建逻辑。
func pointerString(v string) *string {
	return &v
}

// minInt 返回两个整数的较小值，避免重复实现。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
