# 上游 WAF 拦截与出口隧道方案

## 背景

上游天启（`tianqi996.com`）使用**阿里云 ESA** 做防护，按**出口 IP** 做自定义规则拦截。
服务器（`43.166.8.180`，海外机房）访问时返回：

```
HTTP/1.1 200
Server: ESA
X-Tengine-Error: denied by http_custom
```

响应体是**阿里云滑块验证码页**（`initAliyunCaptcha`），而非正常内容。

### 为什么不能靠"纯算"或模拟浏览器通过

| 类型 | 特征 | 能否程序化 |
|---|---|---|
| 算法挑战（arg1 / acw_sc__v2 / 234 / _rand） | 本地 JS 计算 cookie | 可逆向 |
| **阿里云 ESA 滑块（当前遇到）** | `initAliyunCaptcha`、`u_asig` | **不可** |

`u_asig` 由阿里云**服务端私钥签名**，必须在真人滑块验证成功后签发，本地无法计算。
不是逆向深度问题，是信息不在客户端。

### 判定拦截的关键标志

```bash
curl -sS -D - -o /dev/null https://tianqi996.com/ | grep -i x-tengine-error
```

- 返回 `X-Tengine-Error: denied by http_custom` → 出口 IP 被拦
- 无此 header → 正常

## 方案：SSH 反向隧道借家宽出口

家宽（成都电信 `118.114.94.66`）访问天启完全正常，因此让服务器经隧道从家宽出网。

```
hstore 容器
   ↓ HTTPS_PROXY=socks5://172.20.0.1:18080
服务器 0.0.0.0:18080 (sshd 反向转发)
   ↑ SSH 隧道（家宽主动连出，无需公网 IP）
家宽 127.0.0.1:1080 (SOCKS5, waf-socks5.service)
   ↓
天启 tianqi996.com  ✅
```

### 家宽侧（`supconcld`, 10.0.0.86）

两个 systemd user 服务，均 `enable` 且 `Linger=yes`：

- `~/.config/systemd/user/waf-socks5.service` → `/home/beaconcld/socks5.py`，监听 `127.0.0.1:1080`
- `~/.config/systemd/user/waf-tunnel.service` → `ssh -N -R 0.0.0.0:18080:127.0.0.1:1080 root@43.166.8.180`

```bash
systemctl --user status waf-socks5 waf-tunnel
systemctl --user restart waf-tunnel    # 隧道断开时
journalctl --user -u waf-tunnel -n 50
```

### 服务器侧

1. `/etc/ssh/sshd_config.d/99-tunnel.conf`：
   ```
   GatewayPorts clientspecified
   ```
   否则反向隧道只能绑 `127.0.0.1`，容器访问不到。

2. 家宽公钥写入 `/root/.ssh/authorized_keys`。

3. ufw 放行 docker 网段（**必需**，否则容器连不上宿主端口）：
   ```bash
   ufw allow from 172.20.0.0/16 to any port 18080 proto tcp comment "hstore waf tunnel"
   ```

4. `docker-compose.yml`：
   ```yaml
   environment:
     HTTPS_PROXY: "${HTTPS_PROXY:-socks5://172.20.0.1:18080}"
     HTTP_PROXY: "${HTTP_PROXY:-socks5://172.20.0.1:18080}"
     NO_PROXY: "127.0.0.1,localhost,172.20.0.1,.alipay.com,.alipayobjects.com,.aliyuncs.com,.qq.com,.microsoft.com,.office.com,.outlook.com,.live.com"
   ```

> `NO_PROXY` 很重要：家宽上行带宽小，支付宝/邮件等不应绕道家宽，否则会超时。
>
> **Bybit 也必须直连**：家宽 SOCKS5 隧道只放行天启域名，连 `api.bybit.com` 会被
> 直接关闭（`socks connect ... EOF`）。若漏配，USDT 查单会全部失败。

## 代码改动

- `backend/internal/upstream/bybit.go`
  - 新增 `bybitClient`：`Transport.Proxy = nil` 强制直连，不继承 `HTTPS_PROXY`。
    即使运维漏配 `NO_PROXY`，Bybit 查单也不会被隧道劫持。
- `backend/internal/service/crypto.go`
  - `CheckCryptoOrder` 查单失败不再静默返回，改为写日志（过去这类故障完全无痕）。
  - 支持对 `expired` 订单补结算，但仅限 `successAt <= expiresAt`（有效期内付款），
    避免地址回收复用后把晚到账算到别人订单上。
- `backend/cmd/server/main.go`
  - `pendingCryptoOrders` 额外带上 24 小时内过期的订单，轮询中断恢复后能补结算。
- `backend/internal/upstream/tianqi.go`
  - `httpClient` 使用 `http.Transport{Proxy: http.ProxyFromEnvironment}`，支持 `HTTPS_PROXY`
  - 修正「成功但仍被拦截」的误导文案
- `backend/internal/upstream/waf.go`
  - 新增 `blockedByESA`：识别 `X-Tengine-Error: denied` 与 `initAliyunCaptcha` 滑块页，
    在判定成功前直接返回明确错误（原实现会把 `acw_tc` 当成功 Cookie）
  - `RefreshWafCookie` 复用全局 `httpClient`，保证取 Cookie 与接口请求出口一致

## 验证

```bash
# 容器内走隧道访问天启 API（应返回 JSON）
docker run --rm --network container:hstore --entrypoint sh alpine:3.21 -c \
  'apk add -q curl; curl -sS -x socks5h://172.20.0.1:18080 \
   -X POST https://tianqi996.com/api/v3/goods/getDirs \
   -H Content-Type:application/json -d {}'
# 期望: {"code":1001,"msg":"X-APP-ID不能为空"}
```

## 排查

| 现象 | 原因 | 处理 |
|---|---|---|
| 容器连不上 `172.20.0.1:18080` | ufw 未放行 docker 网段 | 执行上面的 `ufw allow` |
| 服务器无 18080 监听 | 隧道断开 / `GatewayPorts` 未配 | 家宽重启 `waf-tunnel`；检查 sshd 配置 |
| 又出现滑块页 | 家宽 IP 变了（动态 IP） | 确认家宽出口 IP，必要时重新加白 |
| 支付宝查账超时 | 走了家宽代理 | 检查 `NO_PROXY` 是否包含 alipay 域名 |
| USDT 链上已到账但不入账 | `NO_PROXY` 缺 `bybit.com`，查单被隧道关闭 | 补上 `,.bybit.com`；日志会打印 `查询 Bybit 充值记录失败` |

## 风险

- **家宽是单点**：设备关机或断网 → 天启同步中断。服务已配 `Restart=always` 自动重连。
- **家宽 IP 动态**：光猫重启可能换 IP，若上游按 IP 放行需重新确认。
- **上行带宽**：仅天启流量走隧道，其余已通过 `NO_PROXY` 排除。
