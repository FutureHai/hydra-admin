# Hydra Admin Console

Go + React 管理台，用于管理 ORY Hydra Client，并查看 Scope、Audience、Token 和审计信息。多个共用数据库、issuer 与密钥的 Hydra 副本可配置为一个逻辑部署。项目基于 Hydra `v26.2.0` 开发。

## 目录

- [功能与当前限制](#功能与当前限制)
- [快速开始](#快速开始)
- [界面预览](#界面预览)
- [直接下载二进制包](#直接下载二进制包)
- [命令与配置](#命令与配置)
- [访问方式](#访问方式)
- [Linux systemd 后台运行](#linux-systemd-后台运行)
- [升级回滚与卸载](#升级回滚与卸载)
- [从源码构建](#从源码构建开发)
- [验证与发布](#验证与发布)
- [排错](#排错)

## 功能与当前限制

可管理 Client，查看从 Client 聚合的 Scope 和 Audience，按已确认能力使用 Token 内省/吊销，并按时间、动作、Client ID 查看审计记录。审计查询每批最多扫描 8 MiB，可通过游标继续查找更早记录；归档仍受 `max_archives` 保留数量限制。支持多个共用 Hydra 数据的 Admin 副本及各副本状态展示。管理员可在页面修改密码；修改后所有会话退出。可配置 Audience 与 Scope 预设、登录锁定、会话时长、API 限流和日志/审计轮转；完整字段及默认值见 [示例配置](configs/config.example.yaml)。

- Client 列表使用 Hydra 游标分页，每页 10 条；Scope / Audience 每次读取最多 100 个 Client，可逐批继续读取并聚合，直到 Hydra 返回末页。读取期间 Client 变化可能影响结果。
- 可在 Client 详情查看和修改 Access Token 策略（JWT、Opaque 或沿用 Hydra 全局策略），编辑高级授权和登出设置，并独立调整 Token 有效期。切换策略前请确认下游支持新签发的 Token 格式。替换整个 Client 的 PUT 和 Secret 轮换关闭；Token 操作按能力探测及配置显示。
- 状态页展示 Hydra 运行版本和当前可用能力；部署前应在目标环境验证所需操作。详见 [开发基准与能力](SUPPORTED_HYDRA_VERSIONS.md)。

多副本探测读取版本信息，并验证健康与 Client 列表可读；无法证明各副本使用同一数据库、issuer 和签名密钥。配置前需在 Hydra 部署侧核对这些值；若不一致，只配置一个副本。当前管理台的登录会话、限流和同一 Client 写入锁均在单进程内，多个 `hydra-admin` 进程不能作为一个共享会话集群直接部署。

## 快速开始

1. 从 [最新 Release](https://github.com/FutureHai/hydra-admin/releases/latest) 下载对应系统的压缩包和同名 `.sha256`，按[下文](#直接下载二进制包)校验并解压。
2. 在交互终端生成密码哈希，保存到 `auth.password_hash_file` 指定的文件（默认 `./state/password.hash`）。
3. 设置 `server.public_base_url`、`hydra.admin_replicas` 中的 Admin 地址，并按[访问方式](#访问方式)检查监听、Cookie 与代理配置。
4. 保护配置文件，在解压目录运行 `./hydra-admin check --config ./config.yaml`，再运行 `./hydra-admin serve --config ./config.yaml`。
5. 在浏览器打开 `server.public_base_url`。Windows 将上述启动命令中的 `./hydra-admin` 换成 `.\hydra-admin.exe`。

`check` 不向 Hydra 发写请求，但会在本地创建日志、审计及密码状态目录，创建并删除临时探针文件，并检查已有文件的写权限及密码哈希格式。密码文件必须存在，相关目录需要可写；只读挂载且没有可写目录时会失败。

## 界面预览

以下截图使用本地测试数据；实际可用操作以能力检测结果为准。点击图片可查看原图。

| 管理员登录 | 控制台概览 | Client 列表 | 创建 Client |
| :---: | :---: | :---: | :---: |
| <a href=".github/screenshots/login.jpg"><img src=".github/screenshots/login.jpg" alt="Hydra Admin 管理员登录页" height="125"></a> | <a href=".github/screenshots/overview.jpg"><img src=".github/screenshots/overview.jpg" alt="Hydra 连接状态与能力矩阵" height="125"></a> | <a href=".github/screenshots/clients.jpg"><img src=".github/screenshots/clients.jpg" alt="包含演示 Client 的列表页" height="125"></a> | <a href=".github/screenshots/create-client.jpg"><img src=".github/screenshots/create-client.jpg" alt="创建 Client 的配置表单" height="125"></a> |

## 直接下载二进制包

无需安装 Go、Node.js 或 npm。打开 [最新 Release](https://github.com/FutureHai/hydra-admin/releases/latest) 或 [全部版本](https://github.com/FutureHai/hydra-admin/releases)，在 **Assets** 中下载与你的系统和 CPU 匹配的压缩包及同名 `.sha256` 文件。发布包内已有网页、示例 `config.yaml` 和许可证材料；只下载可执行文件会缺少配置模板。

| 系统 | CPU | 附件名称中的目标 | 解压格式 |
| --- | --- | --- | --- |
| Linux | x86_64 / aarch64 | `linux-amd64` / `linux-arm64` | tar.gz |
| macOS | Intel / Apple 芯片 | `darwin-amd64` / `darwin-arm64` | tar.gz |
| Windows | x64 / ARM64 | `windows-amd64` / `windows-arm64` | zip |

发布流程为上述六种目标生成压缩包，命名为 `hydra-admin-v<版本>-<目标>.tar.gz` 或 `.zip`。先在 Release 页面确认附件完整；以下示例中的 `X.Y.Z` 改为所下载版本号，并按 CPU 架构选择 `amd64` 或 `arm64`。同名 `.sha256` 校验压缩包，包内 `SHA256SUMS` 校验解压文件。

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

在下载目录打开 PowerShell，按机器架构修改 `$arch`。以下示例先校验压缩包，再解压并逐项核对 `SHA256SUMS`：

<details>
<summary>展开 PowerShell 校验与解压命令</summary>

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

</details>

## 命令与配置

| 命令 | 用途 |
| --- | --- |
| `version` | 输出程序版本、提交、构建时间及 Go 版本。 |
| `hash-password [--algorithm bcrypt]` | 在交互式 TTY 静默读取两次密码并输出哈希；默认 Argon2id，也接受 `--algorithm argon2id`。需 bcrypt 时指定 `--algorithm bcrypt`。无 TTY 会报错并退出 1。 |
| `check --config <路径>` | 校验配置和本地写入路径，执行 Hydra 只读版本/健康/Client 列表探测并输出能力报告。 |
| `serve --config <路径>` | 启动管理台；不带子命令时默认执行 `serve`。 |

`check` 和 `serve` 必须提供 `--config`；命令或参数不正确时退出码为 2。`check` 校验或探测失败时退出码为 1；探测完成时仍会输出 JSON 能力报告。成功时退出码为 0。`serve` 持续运行到服务停止，运行错误返回 1。

### 配置并启动

在**解压后的包目录**先生成密码哈希文件，再编辑 `config.yaml`。至少设置 `server.public_base_url`（浏览器访问管理台的根地址）、`hydra.admin_replicas` 中的 Admin 地址。`hash-password` 会在交互终端静默读取两次密码；配置文件不保存密码哈希。程序根据浏览器地址、监听 IP 和可信代理自动判断访问方式；非回环 HTTP 只需显式设置 `security.allow_insecure_http: true`。Hydra Admin 副本可配置管理台机器能够直接访问的 HTTP/HTTPS 地址，支持 IP 或域名；这里的 HTTP 不需要设置 `security.allow_insecure_http`，该开关只控制浏览器访问管理台。`hydra.public_api_base_url` 若用于 revoke，跨主机访问仍需 HTTPS。

```bash
chmod 600 config.yaml
umask 077
mkdir -p state
./hydra-admin hash-password > state/password.hash
# 如需 bcrypt：./hydra-admin hash-password --algorithm bcrypt > state/password.hash
```

编辑并保存 `config.yaml` 后，在同一目录执行：

```bash
./hydra-admin check --config ./config.yaml
./hydra-admin serve --config ./config.yaml
```

Windows 在 PowerShell 中先创建 `state` 目录，再在交互终端运行 `$hash = .\hydra-admin.exe hash-password`，用 `[System.IO.File]::WriteAllText((Join-Path (Get-Location) 'state\password.hash'), $hash + [Environment]::NewLine, [System.Text.Encoding]::ASCII)` 保存哈希，并将该文件权限限制为运行账号可访问。之后执行 `.\hydra-admin.exe check --config .\config.yaml` 和 `.\hydra-admin.exe serve --config .\config.yaml`。成功后，在浏览器打开 `server.public_base_url`。

常用配置：`auth.session_ttl` 控制会话有效期；`auth.login` 控制登录失败次数与锁定时长；`security.api_rate_limit` 控制读取和写入请求的每 IP 速率；`audit` / `log` 控制文件大小、保留归档数和权限。可选的 `resource_catalog` 为 Audience 设置可选 Scope。当前示例值和全部字段以 [config.example.yaml](configs/config.example.yaml) 为准。

登录后点击顶栏“修改密码”，输入当前密码、新密码和确认密码。新密码至少 12 个字符；修改成功后所有会话失效，需使用新密码登录。密码哈希只保存在 `auth.password_hash_file`（默认 `./state/password.hash`，Unix 权限 `0600`；Windows 应限制为运行账号可访问），页面修改时原子更新。启动必须有有效的哈希文件，升级或备份时须保留；丢失时重新运行 `hash-password` 创建文件。

`GET /healthz` 无需登录，成功时返回 HTTP 200 和 `{"status":"ok"}`；可用于服务存活探针。`GET /readyz` 无需登录，在本地审计写入可用时返回 200，审计故障后返回 503；可对其设置告警。审计故障后会停止新的受审计操作，排查日志并核对可能已在 Hydra 成功的操作后重启服务。两者均不代表 Hydra 就绪或版本受支持；Hydra 状态及禁用原因请看管理台状态页或 `check` 报告。

## 访问方式

访问方式共用同一组配置字段：

| 浏览器访问方式 | `bind_address` | `public_base_url` | `trusted_proxy_cidrs` | `cookie_secure` | `allow_insecure_http` |
| --- | --- | --- | --- | --- | --- |
| 本机回环 HTTP | `127.0.0.1` | `http://127.0.0.1:8085` | `[]` | `false` | `false` |
| 单机内网 IP 直连 | 管理台私有 IP | `http://同一私有IP:8085` | `[]` | `false` | `true` |
| 跨服务器 HTTP 代理 | 管理台内网 IP | `http://浏览器访问域名` | 代理源 IP `/32` | `false` | `true` |
| HTTPS 代理 | 管理台内网 IP | `https://浏览器访问域名` | 代理源 IP CIDR | `true` | `false` |

HTTP 直连时不能信任远程代理 CIDR；回环监听地址也不能配置远程可信代理。跨服务器 HTTP 代理需要将直接连接到管理台的每台远端代理列为单 IP CIDR（IPv4 `/32`、IPv6 `/128`）。

HTTP 会明文传输登录密码和会话 Cookie，应仅在受控网络使用。完成任一种模式的配置后，运行 `./hydra-admin check --config ./config.yaml` 验证；它还要求日志与审计目录可写。

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

其余字段沿用示例配置。此模式仅接受私有 IP，不接受公网 IP、域名或不同端口；请用防火墙只允许需要访问管理台的内网网段连接 8085。

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

此模式只接受 `trusted_proxy_cidrs` 中的直接连接来源；每台远端代理都要单独列出 `/32`（IPv6 为 `/128`），并用防火墙将 8085 端口限制为仅代理服务器可达。

### HTTPS Nginx 反向代理（推荐）

Nginx 与管理台在同一台服务器时，可由 Nginx 接收浏览器的 HTTPS 连接，再转发到本机回环地址。以下域名和证书路径需换成实际值。管理台 `config.yaml` 中设置：

```yaml
server:
  bind_address: "127.0.0.1"
  port: 8085
  public_base_url: "https://admin.example.com"
  trusted_proxy_cidrs:
    - "127.0.0.1/32"
security:
  cookie_secure: true
  allow_insecure_http: false
```

Nginx 示例：

```nginx
server {
    listen 80;
    server_name admin.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    server_name admin.example.com;
    ssl_certificate /etc/letsencrypt/live/admin.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/admin.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8085;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

Nginx 在其他服务器时，将 `bind_address` 改为管理台的可达内网 IP，并在 `trusted_proxy_cidrs` 中填写 Nginx 连接管理台时的实际源 IP；同时用防火墙仅允许代理访问管理台端口。若最外层代理负责 TLS 终止，仍须保证浏览器最终地址为 HTTPS、`public_base_url` 为该地址，并检查直接连接管理台的代理源 IP 与转发头。

## Linux systemd 后台运行

`/opt/hydra-admin` 是指向当前版本的软链接。真实目录保留发布包的解压名称，例如 `/opt/hydra-admin-v2.0.1-linux-amd64`。每个版本保留自己的 `config.yaml` 和 `state/password.hash`。应用日志、审计记录及轮转归档写入当前版本的 `./logs/`。

### 初次安装

按[二进制包下载步骤](#linux)校验压缩包和解压文件，再按[配置并启动](#配置并启动)在解压目录准备 `config.yaml` 和 `state/password.hash`。在包含压缩包和解压目录的下载目录用 Bash 运行以下命令，将 `package` 改成实际的解压目录名称。已有安装的主机使用下方的迁移或升级步骤。

在 `sudoedit` 打开的部署配置中使用以下路径。相对路径以 `config.yaml` 所在的版本目录为基准。

| 配置字段 | 路径 |
| --- | --- |
| `auth.password_hash_file` | `./state/password.hash` |
| `audit.path` | `./logs/audit.jsonl` |
| `log.path` | `./logs/hydra-admin.log` |

```bash
(
  set -eu
  package=hydra-admin-v2.0.1-linux-amd64
  release="/opt/$package"
  for install_path in /opt/hydra-admin "$release"; do
    if sudo test -e "$install_path" || sudo test -L "$install_path"; then
      echo "路径已存在：$install_path，请先确认已有安装或数据。" >&2
      exit 1
    fi
  done
  test -s "$package/config.yaml"
  test -s "$package/state/password.hash"
  if ! getent group hydra-admin >/dev/null 2>&1; then
    sudo groupadd --system hydra-admin
  fi
  if ! id hydra-admin >/dev/null 2>&1; then
    sudo useradd --system --gid hydra-admin --no-create-home --home-dir /opt/hydra-admin --shell /usr/sbin/nologin hydra-admin
  fi
  sudo mkdir -p /opt
  sudo tar -xkzf "$package.tar.gz" -C /opt
  sudo install -d -m 0700 -o hydra-admin -g hydra-admin "$release/state" "$release/logs"
  sudo install -m 0600 -o hydra-admin -g hydra-admin "$package/state/password.hash" "$release/state/password.hash"
  sudo install -m 0640 -o root -g hydra-admin "$package/config.yaml" "$release/config.yaml"
  sudoedit "$release/config.yaml"
  sudo ln -sT -- "$package" /opt/hydra-admin
)
```

用 `sudoedit /etc/systemd/system/hydra-admin.service` 创建服务文件：

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
ReadWritePaths=/opt/hydra-admin/state /opt/hydra-admin/logs
UMask=0077

[Install]
WantedBy=multi-user.target
```

`ReadWritePaths` 允许服务在 `ProtectSystem=strict` 下写入当前版本的 `state/` 和 `logs/`。安装命令会准备这两个目录，权限为 `0700`。应用会在启动时创建缺失的日志和审计文件，权限为 `0600`。

安装、升级和回滚前，按目标版本 `config.yaml` 的 `server.bind_address` 和 `server.port` 填写命令中的 `ready_url`。使用 HTTP 直连应用端口，IPv6 地址用方括号括起。监听所有地址时，填写本机可达的 IP。保存服务文件后执行：

```bash
(
  set -eu
  ready_url='http://127.0.0.1:8085/readyz'  # 按目标版本配置填写实际 IP 和端口
  sudo systemd-analyze verify /etc/systemd/system/hydra-admin.service
  sudo systemctl daemon-reload
  sudo systemctl enable --now hydra-admin
  sudo systemctl status hydra-admin --no-pager
  curl -fsS --noproxy '*' --retry 5 --retry-connrefused -- "$ready_url"
)
```

用 `sudo journalctl -u hydra-admin -n 100 --no-pager` 查看启动错误。可通过 `/opt/hydra-admin/logs/` 查看当前版本的应用日志、审计记录和归档。修改当前版本配置后运行 `sudo systemctl restart hydra-admin`。监听地址与浏览器入口按上面的访问方式配置，Hydra Admin API 应保持在受限网络内。

服务启动后，可执行 `sudo -u hydra-admin /opt/hydra-admin/hydra-admin check --config /opt/hydra-admin/config.yaml` 检查本地路径和 Hydra 的只读连通性。

### 迁移旧安装

从实体安装目录迁移为软链接，或从旧的绝对日志路径迁回版本目录时，执行以下步骤。`package` 填当前版本原来的解压目录名称。若当前配置把日志和审计写到同一个外部目录（例如先前配置的 `/var/log/hydra-admin`），将 `legacy_logs` 设为该目录的绝对路径。已经写入当前版本 `logs/` 时留空。密码文件继续使用当前版本的 `state/password.hash`。

```bash
(
  set -eu
  package=hydra-admin-v2.0.1-linux-amd64
  release="/opt/$package"
  legacy_logs=
  current=$(readlink -e /opt/hydra-admin)
  sudo test -s "$current/state/password.hash"
  if [ -n "$legacy_logs" ]; then
    legacy_logs=$(sudo readlink -e -- "$legacy_logs")
    sudo test -d "$legacy_logs"
    test "$legacy_logs" != "$current/logs"
  fi
  if [ "$current" != "$release" ]; then
    test "$current" = /opt/hydra-admin
    sudo test ! -e "$release"
    sudo test ! -L "$release"
  fi
  sudo systemctl stop hydra-admin
  sudo cp -an -- "$current/config.yaml" "$current/config.yaml.before-migration"
  if [ "$current" != "$release" ]; then
    sudo mv -T -- "$current" "$release"
    sudo ln -sT -- "$package" /opt/hydra-admin
  fi
  if [ -n "$legacy_logs" ]; then
    if sudo test -d "$release/logs"; then
      saved_logs=$(sudo mktemp -d "$release/logs.before-migration.XXXXXX")
      sudo mv -T -- "$release/logs" "$saved_logs/logs"
      printf '原版本日志备份：%s\n' "$saved_logs"
    fi
    sudo cp -aT -- "$legacy_logs" "$release/logs"
  fi
  sudo install -d -m 0700 -o hydra-admin -g hydra-admin "$release/logs"
  sudoedit "$release/config.yaml"
)
```

在 `sudoedit` 打开的配置中应用上表的路径。将现有 unit 更新为上面的示例，并用 `sudo systemctl cat hydra-admin` 检查 drop-in，移除旧的 `LogsDirectory`、`LogsDirectoryMode` 和指向旧目录的 `ReadWritePaths` 条目。然后执行上面的 `systemd-analyze verify`、`daemon-reload` 和启动检查。迁移来源和迁移时生成的备份可继续保留。

## 升级回滚与卸载

### 升级

首次部署目标版本时，按[二进制包下载步骤](#linux)下载并校验新包，在压缩包所在目录运行以下命令。将 `package` 填为目标版本目录名称。目标目录已从校验过的发布包解压时，可直接重跑本段命令，沿用该目录的二进制和配置。新解压的版本沿用当前配置，在 `sudoedit` 中按目标版本要求调整，保留上表的路径。

以服务用户运行的 `check` 通过后，才停服并再次复制密码文件，带上检查期间可能发生的密码修改。然后切换软链接，启动服务并检查 `/readyz`。各版本的日志、审计和轮转归档留在各自的 `logs/` 中。

如果切换软链接后启动或 `/readyz` 检查失败，先[回滚](#回滚)到命令输出的上一版本，修正问题后再重试升级。

```bash
(
  set -eu
  package=hydra-admin-v2.0.2-linux-amd64
  ready_url='http://127.0.0.1:8085/readyz'  # 按目标版本配置填写实际 IP 和端口
  release="/opt/$package"
  test -L /opt/hydra-admin
  previous=$(readlink -e /opt/hydra-admin)
  test "$previous" != "$release"
  sudo test ! -L "$release"
  if ! sudo test -e "$release"; then
    sudo tar -xkzf "$package.tar.gz" -C /opt
    sudo install -m 0640 -o root -g hydra-admin "$previous/config.yaml" "$release/config.yaml"
  else
    sudo test -f "$release/config.yaml"
    sudo test ! -L "$release/config.yaml"
    sudo chown root:hydra-admin "$release/config.yaml"
    sudo chmod 0640 "$release/config.yaml"
  fi
  sudo test -d "$release"
  sudo test -x "$release/hydra-admin"
  sudoedit "$release/config.yaml"
  sudo install -d -m 0700 -o hydra-admin -g hydra-admin "$release/state" "$release/logs"
  sudo -u hydra-admin install -m 0600 "$previous/state/password.hash" "$release/state/password.hash"
  sudo -u hydra-admin "$release/hydra-admin" check --config "$release/config.yaml"
  printf '上一版本：%s\n' "$previous"

  sudo systemctl stop hydra-admin
  sudo -u hydra-admin install -m 0600 "$previous/state/password.hash" "$release/state/password.hash"
  sudo ln -sfnT -- "$package" /opt/hydra-admin
  sudo systemctl start hydra-admin
  sudo systemctl status hydra-admin --no-pager
  curl -fsS --noproxy '*' --retry 5 --retry-connrefused -- "$ready_url"
)
```

### 回滚

将 `package` 填为升级时输出的上一版本目录名称，确认该目录中的 `config.yaml` 使用上表的路径。先把当前密码文件复制到回滚目标，并以服务用户运行 `check`。检查通过后才停服，再同步一次最新密码文件，切回软链接，启动服务并检查 `/readyz`。各版本的日志、审计和轮转归档留在各自的 `logs/` 中。

若停服后、切换链接前中断，确认 `readlink -e /opt/hydra-admin` 仍指向切换前的目录后，可直接启动该版本。

```bash
(
  set -eu
  package=hydra-admin-v2.0.1-linux-amd64
  ready_url='http://127.0.0.1:8085/readyz'  # 按回滚目标配置填写实际 IP 和端口
  release="/opt/$package"
  test -L /opt/hydra-admin
  current=$(readlink -e /opt/hydra-admin)
  test "$current" != "$release"
  sudo test -x "$release/hydra-admin"
  sudo test -s "$current/state/password.hash"
  sudo install -d -m 0700 -o hydra-admin -g hydra-admin "$release/state" "$release/logs"
  sudo -u hydra-admin install -m 0600 "$current/state/password.hash" "$release/state/password.hash"
  sudo -u hydra-admin "$release/hydra-admin" check --config "$release/config.yaml"

  sudo systemctl stop hydra-admin
  sudo -u hydra-admin install -m 0600 "$current/state/password.hash" "$release/state/password.hash"
  sudo ln -sfnT -- "$package" /opt/hydra-admin
  sudo systemctl start hydra-admin
  sudo systemctl status hydra-admin --no-pager
  curl -fsS --noproxy '*' --retry 5 --retry-connrefused -- "$ready_url"
)
```

### 卸载

停用服务，然后删除 systemd unit、drop-in 和 `/opt/hydra-admin` 软链接。各版本目录及其中的配置、密码文件、日志和审计记录都留在原处。

```bash
(
  set -eu
  test -L /opt/hydra-admin
  sudo systemctl disable --now hydra-admin
  sudo rm -f -- /etc/systemd/system/hydra-admin.service
  sudo rm -rf -- /etc/systemd/system/hydra-admin.service.d
  sudo systemctl daemon-reload
  sudo rm -- /opt/hydra-admin
)
```

## 从源码构建（开发）

开发机需要 Go `1.26.1`、Node.js `22.22.3` 和 npm `10.9.8`。从全新检出构建：

```bash
make web-install
make build
cp configs/config.example.yaml config.yaml
umask 077
mkdir -p state
./dist/hydra-admin hash-password > state/password.hash
# 在 config.yaml 填写管理台 URL 与 Hydra Admin 地址
./dist/hydra-admin check --config config.yaml
./dist/hydra-admin serve --config config.yaml
```

示例配置中的密码哈希不可用于登录。建议将 Hydra Admin API 限制在可信网络；跨主机 HTTP 为明文传输，可使用 HTTPS 保护链路。`check` 不向 Hydra 发写请求，但会检查并临时写入本地日志/审计路径；它不代表业务写操作已在目标环境验收。不要提交真实密码哈希、Token、Client Secret、生产地址或日志。

前后端分开开发时，先启动 Go 服务，再在 `web/` 运行 `npm run dev`。Vite 默认使用 `127.0.0.1:5173` 并代理 API 到 `127.0.0.1:8085`；此时本机开发配置的 `server.public_base_url` 应为 `http://127.0.0.1:5173`，`server.bind_address` 为 `127.0.0.1`，`security.cookie_secure` 为 `false`。

## 验证与发布

全新检出先运行 `make web-install` 安装前端依赖；`make test` 会运行 Go 测试、前端测试及类型检查，需要 `web/node_modules`。

```bash
make web-install
make test
make vet
git status --short  # 检查待发布改动；正式发布前将其提交
git tag -a vX.Y.Z -m "vX.Y.Z"
make release VERSION=X.Y.Z
git push origin master vX.Y.Z
```

将 `X.Y.Z` 换成待发布版本。发布构建要求 HEAD 存在同版本 `vX.Y.Z` tag，但不会因本地有未提交改动而拒绝构建；这种本地构建的 `version` 输出会在 commit 后标记 `-dirty`。若要让修改进入 GitHub Release，仍须先提交再创建 tag：推送 tag 后，CI 会从该提交重新构建、运行检查，并在全部平台检查通过后创建 Release 和上传六种包及校验文件。可通过 `./scripts/package-release.sh X.Y.Z windows-amd64` 只构建指定目标；普通分支和 PR 只生成 `0.0.0-ci` 验证包，不发布 Release。

`make release` 会先清理 `dist/` 中所有旧版和本版的发布解包目录、压缩包、同名 `.sha256` 及 `.release-build/`，再生成 Linux、macOS、Windows 的 AMD64/ARM64 六种发布包；`dist/hydra-admin` 等非发布文件不受影响。Linux/macOS 使用 tar.gz，Windows 使用 zip，各包含 `SHA256SUMS`，并附带包外 `.sha256`。Linux 包使用 `CGO_ENABLED=0` 静态构建：CI 在 Ubuntu 20.04 AMD64 容器中检查 Linux AMD64 包无动态加载器并验证服务启动；macOS、Windows runner 验证对应原生包能运行 `version`。其他架构仍需在目标环境验收；浏览器自动化目前使用 Chromium，尚无最低浏览器版本或全部浏览器的兼容性承诺。发布包包含示例配置和许可证信息，部署前必须替换占位值并在目标环境完成验收。构建脚本、测试和 CI 留在本仓库，使独立检出仍可验证和打包。

| 平台或浏览器 | 当前验证范围 |
| --- | --- |
| Linux AMD64 | Ubuntu 20.04 容器启动验证，检查静态链接。 |
| macOS / Windows | CI 对原生包执行 `version`；完整管理流程仍需目标环境验收。 |
| ARM64 包 | 构建并打包；目标设备运行仍需验收。 |
| 浏览器 | 自动化覆盖 Chromium；尚未声明其他浏览器及最低版本。 |

本仓库保留构建运行必需的开发基准 OpenAPI、来源与许可证材料，以及 [开发基准与能力说明](SUPPORTED_HYDRA_VERSIONS.md)。规范的来源、摘要和许可证信息见本仓库的 [SOURCE.md](specs/v26.2.0/SOURCE.md)。

## 排错

| 现象 | 检查方向 |
| --- | --- |
| Client / Token 操作关闭 | 在状态页或 `check` 报告检查健康状态、Client 列表探测、内置规范校验及相应能力的禁用原因。 |
| `web assets are absent` | 当前二进制缺前端产物；运行 `make web-install && make build`，或改用包含网页的 Release 压缩包。 |
| `hash-password requires an interactive TTY` | 在本机交互终端运行；它不接受管道或 CI 标准输入。 |
| `local file check failed` | 检查 `audit.path`、`log.path` 所在目录的写权限，以及已有文件权限是否过宽。 |
| 反向代理后登录或 CSRF 失败 | 核对浏览器最终 URL、`public_base_url`、Cookie 安全设置、直接连接的代理源 IP 和 `X-Forwarded-Proto`。 |

漏洞请按 [安全报告说明](SECURITY.md) 私下报告；贡献前请看 [贡献指南](CONTRIBUTING.md)。发布记录见 [GitHub Releases](https://github.com/FutureHai/hydra-admin/releases) 和 [变更记录](CHANGELOG.md)。

## 许可

本项目代码采用 [MIT 许可证](LICENSE)。依赖和 ORY OpenAPI 材料遵循各自许可证，参见 [第三方声明](THIRD_PARTY_NOTICES.md)。
