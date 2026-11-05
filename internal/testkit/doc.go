// Package testkit 提供集成测试夹具。
//
// 实现只在 `-tags=integration` 下提供；无标签时只剩这个文件，
// 以保证 `go test ./...` 不会因为空包报错。
package testkit
