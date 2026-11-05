package integration

// NewTAPDFetcher 构造 TAPD 需求抓取器（G6）。
func NewTAPDFetcher(baseURL string) *HTTPFetcher {
	return NewHTTPFetcher(baseURL, map[Source]string{
		SourceTAPD: "/tapd/story?url=",
	})
}
