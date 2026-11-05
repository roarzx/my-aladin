package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStaticToken(t *testing.T) {
	authn := StaticToken{Token: "secret", UserID: 9}

	cases := []struct {
		name   string
		header string
		wantOK bool
	}{
		{"valid", "Bearer secret", true},
		{"scheme case-insensitive", "bearer secret", true},
		{"missing", "", false},
		{"wrong token", "Bearer nope", false},
		{"wrong scheme", "Basic secret", false},
		{"prefix only", "Bearer ", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			actor, err := authn.Authenticate(req)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("期望成功: %v", err)
				}
				if actor.UserID != 9 {
					t.Fatalf("UserID = %d", actor.UserID)
				}
				return
			}
			if err == nil {
				t.Fatal("期望失败")
			}
		})
	}
}

func TestStaticTokenNotConfigured(t *testing.T) {
	authn := StaticToken{Token: "", UserID: 1}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer ")
	if _, err := authn.Authenticate(req); err == nil {
		t.Fatal("服务端未配置令牌时必须拒绝")
	}
}
