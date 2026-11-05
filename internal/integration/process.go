package integration

// NewProcessFetcher 构造 Process 需求抓取器（G6）。
func NewProcessFetcher(baseURL string) *HTTPFetcher {
	return NewHTTPFetcher(baseURL, map[Source]string{
		SourceProcess: "/process/requirement?url=",
	})
}
