# 变更记录

已发布版本的完整说明见 [GitHub Releases](https://github.com/FutureHai/hydra-admin/releases)。

## Unreleased

- 同一管理台进程内按 Client 串行写入；Scope / Audience 支持逐批读取 Client 并聚合；审计写入故障会锁定后续受审计操作并使 `/readyz` 返回 503。
- 发布构建要求干净工作树和匹配 tag，tag CI 验证通过后自动创建 Release。
- 创建、查看和编辑 Client 的 `metadata` JSON 业务数据；编辑时通过 `/metadata` JSON Patch 更新，未修改字段不提交。含脱敏值的 Metadata 不自动回填编辑框。
- 创建 Client 时可选填各授权方式的 Token 有效期、授权/登出同意页开关及前后通道登出参数；留空沿用 Hydra 默认值。
- Client 详情支持编辑授权/登出设置，并通过独立操作调整或重置 Token 有效期；提交时保留其它已设置的有效期。
- 整理 README 的快速开始、配置与命令契约，明确 `check` 的本地写入探针和当前功能限制。
- 补充 HTTPS 反向代理、升级回滚、卸载、排错和安全报告说明。

历史版本说明以对应 Release 页面为准；此处不补写未经核实的历史变更。
