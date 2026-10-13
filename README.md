# Mihomo UI

单容器运行 [mihomo](https://github.com/MetaCubeX/mihomo) 内核 + [zashboard](https://github.com/Zephyruso/zashboard) 控制面板：配置管理、节点切换、连接与日志，开箱即用。

镜像：`ghcr.io/bzmshang/mihomo-ui`（`linux/amd64` / `linux/arm64`）

---

## 快速开始

```bash
docker run -d --name mihomo-ui \
  --network host --cap-add NET_ADMIN \
  --device /dev/net/tun:/dev/net/tun \
  -e UI_LISTEN=0.0.0.0:7080 \
  -e UI_PASSWORD=mihomo-ui \
  -v "$PWD/data:/data/mihomo-ui" \
  ghcr.io/bzmshang/mihomo-ui
```

或 Compose：

```yaml
services:
  app:
    image: ghcr.io/bzmshang/mihomo-ui
    restart: unless-stopped
    network_mode: host
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/net/tun:/dev/net/tun
    environment:
      - UI_LISTEN=0.0.0.0:7080
      - UI_PASSWORD=mihomo-ui
    volumes:
      - ./data:/data/mihomo-ui
```

```bash
docker compose up -d
```

| 入口 | 地址 | 说明 |
|------|------|------|
| 面板 | http://127.0.0.1:7080 | 密码即 `UI_PASSWORD` |
| 代理 | `127.0.0.1:7890` | mixed-port（HTTP / SOCKS5），来自订阅 |
| 内核 API | `127.0.0.1:9090` | 面板连内核用，一般不直接访问 |

登录面板后在「配置」页添加订阅或上传 YAML，点卡片激活即生效。

> WSL 下开启 TUN 可能与 Windows 冲突，按需使用。

---

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `UI_PASSWORD` | `mihomo-ui` | 面板登录密码，**公网可达时务必修改** |
| `UI_LISTEN` | `0.0.0.0:7080` | 面板监听（host 网络下即本机端口） |
| `MIHOMO_API` | `127.0.0.1:9090` | 内核控制 API 监听（面板连内核用） |
| `MIHOMO_SECRET` | 每次启动随机 | 内核 API 密钥；要用别的客户端连内核时才需固定 |
| `DATA_HOME` | `data`（镜像内 `/data/mihomo-ui`） | 数据目录（配置、设置、内核 home） |
| `MIHOMO_BIN` | `./mihomo`（镜像内 `/mihomo`） | 内核二进制路径 |
| `TZ` | `Asia/Shanghai` | 时区 |

常规使用只需设 `UI_PASSWORD`，其余都有可用默认值。

- `UI_LISTEN` 与 `MIHOMO_API` 都是 `host:port`，格式不对启动即报错并指明是哪个变量
- `MIHOMO_API` 撞端口时改它：9090 被占（Prometheus 等）会导致启动失败，日志会直说
- `MIHOMO_SECRET` 不设则每次启动随机，可从 `data/mihomo/config.yaml` 的 `secret` 读到

---

## 配置如何生效

```text
mihomo/config.yaml = 当前配置 <- override.yaml <- settings 开关 <- 系统强制
```

```text
data/
  mihomo/config.yaml      # 最终配置
  ui/
    override.yaml         # 覆盖配置
    settings.yaml         # 面板配置
    config/<id>.yaml      # 各配置原始内容
```

---

## 面板

面板是 zashboard，用它的**发布构建**而不是从源码重建：它是一个带自己工具链的大型 Vue 应用，锁定版本加校验和能让面板成为本仓库的一个固定输入，而不是一个会漂移的依赖。

```bash
bash scripts/fetch-zashboard.sh        # 或 pwsh -File scripts/fetch-zashboard.ps1
go -C backend run ./cmd/server
```

产物落在 `backend/internal/web/dist`（不入库，由 `go:embed` 打进二进制）。换版本时同时改脚本里的 `VERSION` 与 `SHA256`——校验和才是这个锁定生效的地方。

### 浏览器怎么连上内核

zashboard 是 Clash API 客户端，会把拿到的凭据存进 localStorage。面板因此做了一层同源代理：

- 面板在 `/clash/*` 上转发内核控制 API，把浏览器的**会话令牌**换成**内核密钥**再发出，浏览器永远拿不到内核密钥
- 启动脚本（`backend/internal/web/panel-bootstrap.js`）在 zashboard 加载前把这个部署写成它唯一的后端，并带上 `disableUpgradeCore` 与 `disableTunMode`：就地升级内核会替换掉本面板监管、并且自己管理 `config.yaml` 的子进程；TUN 开关则由面板设置强制，写在这里会在下次应用时被静默改回
- 面板有密码，而 zashboard 没有登录页，所以登录表单由启动脚本叠在应用之上；它不能放在单独的路由上——service worker 会把任何导航都回成 index.html
- 会话是内存态，重启即失效；`UI_PASSWORD` 为空时不发令牌，面板直接可用

`/clash/*` 与 `/api/*` 一样需要令牌。唯一例外是 WebSocket 握手允许用 `?token=`，因为浏览器无法给 WebSocket 设置请求头；其余请求的凭据一律走 `Authorization`，不进 URL。
