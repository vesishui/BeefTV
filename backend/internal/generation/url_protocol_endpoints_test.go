package generation

import "testing"

// 用户既可能填厂商根地址，也可能直接粘贴文档里的 Base URL（含 /api/v1 或 /v1）。
// 两种写法都必须拼出同一个上游端点，且成片下载要判定为同源（否则不会带 Bearer）。
func TestInstalledVideoProtocolEndpointsAcceptDocumentedBaseURLForms(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		path    string
		want    string
		origin  string
		rawURL  string
		sameOrg bool
	}{
		{
			name: "派普根地址", base: "https://api.paipu.net", path: "/v1/videos",
			want:   "https://api.paipu.net/v1/videos",
			origin: "https://api.paipu.net", rawURL: "https://api.paipu.net/v1/videos/t1/content", sameOrg: true,
		},
		{
			name: "派普含版本前缀", base: "https://api.paipu.net/v1", path: "/v1/videos",
			want:   "https://api.paipu.net/v1/videos",
			origin: "https://api.paipu.net/v1", rawURL: "https://api.paipu.net/v1/videos/t1/content", sameOrg: true,
		},
		{
			name: "山海根地址", base: "https://shanhai.vnshu.cn", path: "/api/v1/generations",
			want:   "https://shanhai.vnshu.cn/api/v1/generations",
			origin: "https://shanhai.vnshu.cn", rawURL: "https://shanhai.vnshu.cn/api/v1/media/runs/run_1", sameOrg: true,
		},
		{
			name: "山海文档 Base URL", base: "https://shanhai.vnshu.cn/api/v1", path: "/api/v1/generations",
			want:   "https://shanhai.vnshu.cn/api/v1/generations",
			origin: "https://shanhai.vnshu.cn/api/v1", rawURL: "https://shanhai.vnshu.cn/api/v1/media/runs/run_1", sameOrg: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := APIURL(test.base, test.path); got != test.want {
				t.Fatalf("APIURL(%q, %q) = %q, want %q", test.base, test.path, got, test.want)
			}
			if got := SameProviderOrigin(test.origin, test.rawURL); got != test.sameOrg {
				t.Fatalf("SameProviderOrigin(%q, %q) = %v, want %v（决定成片下载是否带 Bearer）", test.origin, test.rawURL, got, test.sameOrg)
			}
		})
	}
}
