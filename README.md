# Hydra Admin Console

Go + React 管理台，用于管理受支持的 ORY Hydra Client，并查看 Scope、Audience、Token 和审计信息。多个共用数据库、issuer 与密钥的 Hydra 副本可配置为一个逻辑部署。当前固定支持 Hydra `v26.2.0`；运行版本不匹配时会关闭 Client 和 Token 业务操作。

## 直接下载二进制包

无需安装 Go、Node.js 或 npm。打开 [GitHub Releases](https://github.com/FutureHai/hydra-admin/releases)，选择一个版本，在 **Assets** 中下载与你的系统和 CPU 匹配的压缩包及同名 `.sha256` 文件。发布包内已有网页、示例 `config.yaml` 和许可证材料；只下载可执行文件会缺少配置模板。

| 系统 | CPU | 附件名称中的目标 | 解压格式 |
| --- | --- | --- | --- |
| Linux | x86_64 / aarch64 | `linux-amd64` / `linux-arm64` | tar.gz |
| macOS | Intel / Apple 芯片 | `darwin-amd64` / `darwin-arm64` | tar.gz |
| Windows | x64 / ARM64 | `windows-amd64` / `windows-arm64` | zip |

每个版本按上述六种目标提供压缩包，命名为 `hydra-admin-v<版本>-<目标>.tar.gz` 或 `.zip`。以下示例中的 `X.Y.Z` 改为所下载 Release 的版本号，并按 CPU 架构选择 `amd64` 或 `arm64`。

### Linux

在下载目录中执行；`x86_64` 选 `amd64`，`aarch64` 选 `arm64`：

```bash
VERSION=X.Y.Z  # 改成 Assets 中已发布的版本
ARCH=amd64     # ARM64 机器改成 arm64
PACKAGE="hydra-admin-v${VERSION}-linux-${ARCH}"
sha256sum -c "${PACKAGE}.tar.gz.sha256"
tar -xzf "${PACKAGE}.tar.gz"
cd "${PACKAGE}"
sha256sum -c SHA256SUMS
./hydra-admin version
```

### macOS

在下载目录中执行；Apple 芯片选 `arm64`，Intel 选 `amd64`：

```bash
VERSION=X.Y.Z  # 改成 Assets 中有 macOS 附件的版本
ARCH=arm64     # Intel Mac 改成 amd64
PACKAGE="hydra-admin-v${VERSION}-darwin-${ARCH}"
shasum -a 256 -c "${PACKAGE}.tar.gz.sha256"
tar -xzf "${PACKAGE}.tar.gz"
cd "${PACKAGE}"
shasum -a 256 -c SHA256SUMS
./hydra-admin version
```

### Windows

在下载目录打开 PowerShell，按机器架构修改 `$arch`：

```powershell
$version = 'X.Y.Z' # 改成 Assets 中有 Windows zip 的版本
$arch = 'amd64'    # ARM64 机器改成 arm64
$package = "hydra-admin-v${version}-windows-${arch}"
$archive = "${package}.zip"
$expected = ((Get-Content "${archive}.sha256" -Raw) -split '\s+')[0]
if ((Get-FileHash $archive -Algorithm SHA256).Hash -ne $expected) { throw '压缩包校验失败' }
Expand-Archive -Path $archive -DestinationPath .
Set-Location $package
Get-Content SHA256SUMS | ForEach-Object {
  if ($_ -notmatch '^([0-9a-f]{64})  (.+)$') { throw '包内校验文件格式错误' }
  $hash = $Matches[1]
  $path = $Matches[2]
  if ((Get-FileHash $path -Algorithm SHA256).Hash -ne $hash) { throw "文件校验失败: $path" }
}
.\hydra-admin.exe version
```

### 配置并启动

在**解压后的包目录**先生成密码哈希，再编辑 `config.yaml`。至少设置 `server.public_base_url`（浏览器访问管理台的根地址）、`hydra.admin_replicas` 中的 Admin 地址和 `auth.password_hash`。`hash-password` 会在交互终端静默读取两次密码；把输出的哈希填入配置，示例哈希不能登录。程序根据浏览器地址、监听 IP 和可信代理自动判断访问方式；非回环 HTTP 只需显式设置 `security.allow_insecure_http: true`。Hydra Admin API 需从运行管理台的机器可达，且应限制在可信网络。

```bash
chmod 600 config.yaml
./hydra-admin hash-password
```

编辑并保存 `config.yaml` 后，在同一目录执行：

```bash
./hydra-admin check --config ./config.yaml
./hydra-admin serve --config ./config.yaml
```

Windows 在 PowerShell 中运行 `.\hydra-admin.exe hash-password`，把哈希写入 `config.yaml` 并限制该文件的访问权限，再执行 `.\hydra-admin.exe check --config .\config.yaml` 和 `.\hydra-admin.exe serve --config .\config.yaml`。`check` 只执行本地配置检查和 Hydra 只读探测；成功后，在浏览器打开 `server.public_base_url`。

访问方式共用同一组配置字段：

| 浏览器访问方式 | `bind_address` | `public_base_url` | `trusted_proxy_cidrs` | `cookie_secure` | `allow_insecure_http` |
| --- | --- | --- | --- | --- | --- |
| 本机回环 HTTP | `127.0.0.1` | `http://127.0.0.1:8085` | `[]` | `false` | `false` |
| 单机内网 IP 直连 | 管理台私有 IP | `http://同一私有IP:8085` | `[]` | `false` | `true` |
| 跨服务器 HTTP 代理 | 管理台内网 IP | `http://浏览器访问域名` | 代理源 IP `/32` | `false` | `true` |
| HTTPS 代理 | 管理台内网 IP | `https://浏览器访问域名` | 代理源 IP CIDR | `true` | `false` |

### 单机内网 IP 与端口直连

管理台和 Hydra 安装在同一台服务器，浏览器能直接访问管理台服务器的私有 IP 时，可在 `config.yaml` 中设置以下字段。示例中的 `10.0.0.12` 要替换为该服务器实际的内网 IP；访问地址中的 IP 和端口必须与监听地址及端口一致：

```yaml
server:
  bind_address: "10.0.0.12"
  port: 8085
  public_base_url: "http://10.0.0.12:8085"
  trusted_proxy_cidrs: []
security:
  cookie_secure: false
  allow_insecure_http: true
```

其余字段沿用示例配置。此模式仅接受私有 IP，不接受公网 IP、域名或不同端口；请用防火墙只允许需要访问管理台的内网网段连接 8085。HTTP 会明文传输登录密码和会话 Cookie。使用包含此配置项的二进制，运行 `./hydra-admin check --config ./config.yaml` 验证。

### 跨服务器 HTTP Nginx 代理

只有在浏览器最终访问地址确实为 HTTP 时，才开启此模式。以下示例中 `10.0.0.12` 是管理台服务器内网 IP，`10.0.0.11` 是 Nginx 连接管理台时的**实际源 IP**。在管理台服务器的 `config.yaml` 中设置：

```yaml
server:
  bind_address: "10.0.0.12"
  port: 8085
  public_base_url: "http://admin.example.com"
  trusted_proxy_cidrs:
    - "10.0.0.11/32"
security:
  cookie_secure: false
  allow_insecure_http: true
```

在 Nginx 服务器配置反向代理；其余 `config.yaml` 字段仍按示例文件填写：

```nginx
server {
    listen 80;
    server_name admin.example.com;

    location / {
        proxy_pass http://10.0.0.12:8085;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

此模式只接受 `trusted_proxy_cidrs` 中的直接连接来源；每台远端代理都要单独列出 `/32`（IPv6 为 `/128`），并用防火墙将 8085 端口限制为仅代理服务器可达。HTTP 会明文传输登录密码和会话 Cookie，因此只应在受控网络内使用。使用包含此配置项的二进制，运行 `./hydra-admin check --config ./config.yaml` 验证。

## Linux systemd 后台运行

以下命令在**已解压并配置好的 Linux 发布包目录**执行，适用于使用 `systemd` 的发行版。先完成上面的密码、Hydra 地址和管理台 URL 配置，并确认 `./hydra-admin check --config ./config.yaml` 通过。示例把程序装在 `/opt/hydra-admin`，以专用用户运行；`audit.path` 和 `log.path` 保持示例配置中的 `./logs/...`。

```bash
if ! getent group hydra-admin >/dev/null 2>&1; then
  sudo groupadd --system hydra-admin
fi
if ! id hydra-admin >/dev/null 2>&1; then
  sudo useradd --system --gid hydra-admin --no-create-home --home-dir /opt/hydra-admin --shell /usr/sbin/nologin hydra-admin
fi
sudo install -d -m 0755 -o root -g root /opt/hydra-admin
sudo install -m 0755 -o root -g root ./hydra-admin /opt/hydra-admin/hydra-admin
sudo install -m 0600 -o hydra-admin -g hydra-admin ./config.yaml /opt/hydra-admin/config.yaml
sudo cp -R ./LICENSE ./SUPPORTED_HYDRA_VERSIONS.md ./THIRD_PARTY_NOTICES.md ./licenses /opt/hydra-admin/
sudo install -d -m 0700 -o hydra-admin -g hydra-admin /opt/hydra-admin/logs
sudo -u hydra-admin sh -c 'cd /opt/hydra-admin && ./hydra-admin check --config ./config.yaml'
```

创建 `/etc/systemd/system/hydra-admin.service`：

```ini
[Unit]
Description=Hydra Admin Console
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=hydra-admin
Group=hydra-admin
WorkingDirectory=/opt/hydra-admin
ExecStart=/opt/hydra-admin/hydra-admin serve --config /opt/hydra-admin/config.yaml
Restart=on-failure
RestartSec=5s
TimeoutStopSec=35s
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/opt/hydra-admin/logs
UMask=0077

[Install]
WantedBy=multi-user.target
```

`UMask=0077` 与配置中的 `audit.file_mode`、`log.file_mode: "0600"` 相容。若修改日志或审计文件路径，要同步调整 `ReadWritePaths` 和目录权限。保存服务文件后执行：

```bash
sudo systemd-analyze verify /etc/systemd/system/hydra-admin.service
sudo systemctl daemon-reload
sudo systemctl enable --now hydra-admin
sudo systemctl status hydra-admin --no-pager
curl -f http://127.0.0.1:8085/healthz
```

用 `sudo journalctl -u hydra-admin -n 100 --no-pager` 查看启动错误；应用日志和审计记录写入 `/opt/hydra-admin/logs/`。修改配置或更换二进制后运行 `sudo systemctl restart hydra-admin`。此示例只监听本机回环地址；如果要让其他设备访问，请通过 HTTPS 反向代理暴露管理台，并保持 Hydra Admin API 不对公网开放。

## 从源码构建（开发）

开发机需要 Go `1.26.1`、Node.js `22.22.3` 和 npm `10.9.8`。从全新检出构建：

```bash
make web-install
make build
cp configs/config.example.yaml config.yaml
./dist/hydra-admin hash-password
# 将生成的哈希写入 config.yaml，并填写管理台 URL 与 Hydra Admin 地址
./dist/hydra-admin check --config config.yaml
./dist/hydra-admin serve --config config.yaml
```

示例配置中的密码哈希不可用于登录。将 Hydra Admin API 限制在可信网络；跨主机连接需要有效的 HTTPS 证书。`check` 只执行配置校验和只读探测，不代表写操作已在目标环境验收。不要提交真实密码哈希、Token、Client Secret、生产地址或日志。

前后端分开开发时，先启动 Go 服务，再在 `web/` 运行 `npm run dev`。Vite 默认使用 `127.0.0.1:5173` 并代理 API 到 `127.0.0.1:8085`；此时本机开发配置的 `server.public_base_url` 应为 `http://127.0.0.1:5173`，`server.bind_address` 为 `127.0.0.1`，`security.cookie_secure` 为 `false`。

## 验证与发布

```bash
make test
make vet
make release VERSION=0.1.0
```

`make release` 默认生成 Linux、macOS、Windows 的 AMD64/ARM64 六种发布包；Linux/macOS 使用 tar.gz，Windows 使用 zip，各包含 `SHA256SUMS`，并附带包外 `.sha256`。可通过 `./scripts/package-release.sh 0.1.0 windows-amd64` 只构建指定目标。CI 在 Ubuntu 20.04 AMD64 容器中验证 Linux 包的服务启动，并在 macOS、Windows runner 上验证对应原生包能运行 `version`；其他架构仍需在目标环境验收。发布包包含示例配置和许可证信息，部署前必须替换占位值并在目标环境完成验收。构建脚本、测试和 CI 留在本仓库，使独立检出仍可验证和打包。

本仓库保留构建运行必需的固定 OpenAPI、来源与许可证材料，以及 [支持版本说明](SUPPORTED_HYDRA_VERSIONS.md)。

## 许可

本项目代码采用 [MIT 许可证](LICENSE)。依赖和 ORY OpenAPI 材料遵循各自许可证，参见 [第三方声明](THIRD_PARTY_NOTICES.md)。
