# 贡献指南

欢迎提交可复现的问题和范围明确的改动。安全漏洞请按 [SECURITY.md](SECURITY.md) 私密报告。

## 本地准备

使用 [README](README.md) 所列的 Go、Node.js 和 npm 版本。全新检出后运行：

```bash
make web-install
make test
make vet
```

`make test` 会运行 Go 单元测试、前端测试和类型检查。需要构建完整二进制时运行 `make build`；前端产物会嵌入程序。

可在隔离的 Hydra v26.2.0 测试环境运行真实 API 集成测试；测试会创建并删除随机 Client，不要指向生产环境。需要验证 Token 与 JWT 格式时，Public API 也必须可用，且 JWT 测试要求 Hydra 的 JWKS 和签名密钥正常工作：

```bash
HYDRA_ADMIN_TEST_URL=http://127.0.0.1:4445 \
HYDRA_PUBLIC_TEST_URL=http://127.0.0.1:4444 \
go test ./internal/hydra -run '^TestLiveHydra' -count=1 -v
```

## 提交改动

- 描述问题、行为变化及验证方法；涉及界面时可附截图。
- 涉及 Hydra API 时，以仓库内 v26.2.0 开发基准规范和适配器白名单为依据，补充目标环境的测试。能力探测只使用已审阅的只读请求。
- 保持认证、CSRF、能力门控和审计边界。不要提交真实密码哈希、Token、Client Secret、生产地址、日志或审计文件。
- 修改用户可见行为时同步更新 README、[开发基准与能力说明](SUPPORTED_HYDRA_VERSIONS.md) 或[变更记录](CHANGELOG.md)。
