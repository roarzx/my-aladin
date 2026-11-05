package main

import "testing"

func TestSliceUTF8(t *testing.T) {
	content := "你好世界abc"
	cases := []struct {
		name   string
		offset int
		limit  int
		want   string
	}{
		{"从头读取", 0, 6, "你好"},
		{"对齐到字符边界", 1, 6, "好世"},
		{"截断到末尾之外", 0, 999, content},
		{"零 limit 表示到末尾", 6, 0, "世界abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sliceUTF8(content, tc.offset, tc.limit)
			if err != nil {
				t.Fatalf("sliceUTF8: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	if _, err := sliceUTF8(content, -1, 3); err == nil {
		t.Fatal("负 offset 应报错")
	}
	if _, err := sliceUTF8(content, len(content)+1, 3); err == nil {
		t.Fatal("越界 offset 应报错")
	}
}
