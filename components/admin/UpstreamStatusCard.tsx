"use client";

import { Button, Card, Message, Spin, Tag } from "@arco-design/web-react";
import { IconRefresh } from "@arco-design/web-react/icon";
import { useCallback, useEffect, useRef, useState } from "react";

interface ProxyStatus {
  configured: boolean;
  address: string;
  protocol: string;
  reachable: boolean;
  latencyMs: number;
  error?: string;
}

interface UpstreamProbe {
  checked: boolean;
  ok: boolean;
  blocked: boolean;
  latencyMs: number;
  message?: string;
}

interface PoolNode {
  index: number;
  address: string;
  healthy: boolean;
  latencyMs: number;
  lastError?: string;
  total: number;
  success: number;
  failed: number;
  lastProbe?: string;
}

interface PoolStatus {
  statsAddress: string;
  listen: string;
  reached: boolean;
  error?: string;
  /** 后端可能返回 null（nil 切片），渲染时一律按空数组处理 */
  nodes: PoolNode[] | null;
  healthy: number;
  totalNodes: number;
}

interface OnboardCommand {
  index: number;
  port: number;
  shell: string;
}

interface StatusData {
  proxy: ProxyStatus;
  upstream: UpstreamProbe;
  checkedAt: number;
  /** 上游真实请求完成时间；仅 probe=1 时有值 */
  upstreamAt: number;
}

function proxyTag(p: ProxyStatus) {
  if (!p.configured) return { color: "gray", text: "未配置代理" };
  if (p.reachable) return { color: "green", text: "隧道正常" };
  return { color: "red", text: "隧道不可用" };
}

function upstreamTag(u: UpstreamProbe) {
  if (!u.checked) return { color: "gray", text: "未检测" };
  if (u.ok) return { color: "green", text: "接口正常" };
  if (u.blocked) return { color: "orange", text: "被 WAF 拦截" };
  return { color: "red", text: "接口异常" };
}

/**
 * 管理员首页的出口代理状态卡片。
 *
 * 上游天启按出口 IP 拦截，生产环境通过家宽设备的反向隧道访问；
 * 家宽关机/断网时隧道端口消失，商品同步与下单会一起失败。
 * 这里直接展示容器实际使用的 HTTPS_PROXY 是否可连，以及真实上游请求的结果。
 */
export function UpstreamStatusCard() {
  const [data, setData] = useState<StatusData | null>(null);
  const [pool, setPool] = useState<PoolStatus | null>(null);
  const [commands, setCommands] = useState<OnboardCommand[]>([]);
  const [loading, setLoading] = useState(true);
  const [probing, setProbing] = useState(false);
  const probingRef = useRef(false);

  const load = useCallback(async (probe: boolean) => {
    if (probe) {
      probingRef.current = true;
      setProbing(true);
    }
    try {
      const res = await fetch(`/api/admin/upstream/status${probe ? "?probe=1" : ""}`, { cache: "no-store" });
      const j = await res.json();
      if (j.ok) {
        setData((prev) => ({
          proxy: j.proxy,
          // 轻量刷新（只查隧道）时保留上一次的上游探测结果，避免卡片闪烁
          upstream: probe ? j.upstream : (prev?.upstream ?? j.upstream),
          checkedAt: j.checkedAt,
          upstreamAt: probe ? j.upstreamAt : (prev?.upstreamAt ?? 0),
        }));
      } else {
        Message.error(j.message ?? "获取代理状态失败");
      }
    } catch {
      Message.error("获取代理状态失败");
    } finally {
      setLoading(false);
      if (probe) {
        probingRef.current = false;
        setProbing(false);
      }
    }
  }, []);

  const loadPool = useCallback(async () => {
    try {
      const j = await fetch("/api/admin/pool", { cache: "no-store" }).then((r) => r.json());
      if (j.ok) {
        setPool(j.pool as PoolStatus);
        setCommands((j.commands as OnboardCommand[]) ?? []);
      }
    } catch {
      // 节点池读取失败不影响主卡片
    }
  }, []);

  useEffect(() => {
    loadPool();
    const poolTimer = setInterval(loadPool, 30000);
    return () => clearInterval(poolTimer);
  }, [loadPool]);

  useEffect(() => {
    load(true);
    // 每 60 秒只探测隧道端口，成本极低；上游探测仅手动触发，避免频繁刷新 WAF Cookie
    const timer = setInterval(() => {
      if (!probingRef.current) load(false);
    }, 60000);
    return () => clearInterval(timer);
  }, [load]);

  const proxy = data?.proxy;
  const upstream = data?.upstream;
  // 统一在这里兜底：后端可能把空切片序列化成 null
  const nodes = pool?.nodes ?? [];
  const pTag = proxy ? proxyTag(proxy) : { color: "gray", text: "检测中" };
  const uTag = upstream ? upstreamTag(upstream) : { color: "gray", text: "检测中" };

  return (
    <Card
      bordered={false}
      title="上游出口 / 家宽代理"
      extra={
        <div className="flex items-center gap-2 text-[12px] text-[#86909c]">
          {probing ? <span>检测中…</span> : data ? <span>{new Date(data.checkedAt).toLocaleTimeString("zh-CN")}</span> : null}
          <Button size="mini" icon={<IconRefresh />} loading={loading || probing} onClick={() => load(true)}>
            重新检测
          </Button>
        </div>
      }
    >
      {loading && !data ? (
        <div className="py-4 text-center">
          <Spin size={16} />
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px]">
            <Tag color={pTag.color}>{pTag.text}</Tag>
            <span className="text-[#4e5969]">
              代理 <span className="font-mono">{proxy?.address || "—"}</span>
              {proxy?.protocol && proxy.protocol !== "unknown" ? `（${proxy.protocol}）` : ""}
            </span>
            {proxy?.reachable && <span className="text-[#86909c]">握手 {proxy.latencyMs} ms</span>}
            {proxy?.error && <span className="text-[#f53f3f]">{proxy.error}</span>}
          </div>

          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px]">
            <Tag color={uTag.color}>{uTag.text}</Tag>
            <span className="text-[#4e5969]">上游 API 真实请求</span>
            {upstream?.latencyMs ? <span className="text-[#86909c]">{upstream.latencyMs} ms</span> : null}
            {data?.upstreamAt ? (
              <span className="text-[#86909c]">检测于 {new Date(data.upstreamAt).toLocaleTimeString("zh-CN")}</span>
            ) : null}
            {upstream?.message && (
              <span className={upstream.ok ? "text-[#86909c]" : "text-[#f53f3f]"}>{upstream.message}</span>
            )}
          </div>

          {!proxy?.configured && (
            <div className="text-[11px] leading-relaxed text-[#86909c]">
              未检测到 HTTPS_PROXY。若服务器出口 IP 被上游 WAF 拦截，需要在 docker-compose 中配置家宽 SOCKS5 隧道。
            </div>
          )}
          {proxy?.configured && !proxy.reachable && (
            <div className="text-[11px] leading-relaxed text-[#d25f00]">
              隧道不可用：请检查家宽设备是否开机、代理进程是否运行，以及服务器 18080 端口是否监听。
            </div>
          )}
          {upstream?.blocked && (
            <div className="text-[11px] leading-relaxed text-[#d25f00]">
              代理可用但上游返回人机校验页：家宽出口 IP 可能变了，需要重新加白或更换出口。
            </div>
          )}

          {/* 下游家宽节点池：一个端口收口多个家宽，任一存活即可用 */}
          <div className="border-t border-[#f2f3f5] pt-3">
            <div className="mb-2 flex flex-wrap items-center gap-2">
              <span className="text-[13px] font-medium text-[#1d2129]">下游家宽节点</span>
              {pool ? (
                pool.reached ? (
                  <Tag size="small" color={pool.healthy > 0 ? "green" : "red"}>
                    {pool.healthy} / {pool.totalNodes} 在线
                  </Tag>
                ) : (
                  <Tag size="small" color="red">聚合器未运行</Tag>
                )
              ) : (
                <Tag size="small" color="gray">检测中</Tag>
              )}
              <span className="text-[11px] text-[#86909c]">
                容器统一走 {pool?.listen || "0.0.0.0:18080"}，自动故障转移
              </span>
            </div>

            {pool?.error && (
              <div className="mb-2 text-[11px] leading-relaxed text-[#f53f3f]">{pool.error}</div>
            )}

            <div className="flex flex-col gap-1.5">
              {nodes.map((n) => (
                <div
                  key={n.address}
                  className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded bg-[#f7f8fa] px-2.5 py-1.5 text-[12px]"
                >
                  <Tag size="small" color={n.healthy ? "green" : "red"}>
                    {n.healthy ? "在线" : "离线"}
                  </Tag>
                  <span className="text-[#4e5969]">
                    节点 {n.index} <span className="font-mono text-[#86909c]">:{18080 + n.index}</span>
                  </span>
                  {n.healthy && <span className="text-[#86909c]">{n.latencyMs} ms</span>}
                  {n.total > 0 && (
                    <span className="text-[#86909c]">
                      连接 {n.total}（成功 {n.success} / 失败 {n.failed}）
                    </span>
                  )}
                  {n.lastError && (
                    <span className="truncate text-[#f53f3f]" title={n.lastError}>
                      {n.lastError}
                    </span>
                  )}
                </div>
              ))}
              {pool && nodes.length === 0 && (
                <div className="text-[11px] text-[#86909c]">尚未配置任何节点。</div>
              )}
            </div>

            {commands.length > 0 && (
              <div className="mt-3">
                <div className="mb-1 text-[12px] text-[#4e5969]">
                  一键接入命令（在新家宽设备上以 root 执行）
                </div>
                <div className="flex flex-col gap-1.5">
                  {commands.slice(0, 4).map((c) => (
                    <div key={c.index} className="flex items-center gap-2">
                      <span className="w-[64px] shrink-0 text-[11px] text-[#86909c]">
                        节点 {c.index}
                      </span>
                      <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap rounded bg-[#f2f3f5] px-2 py-1 font-mono text-[11px] text-[#4e5969]">
                        {c.shell}
                      </code>
                      <Button
                        size="mini"
                        onClick={() => {
                          void navigator.clipboard?.writeText(c.shell);
                          Message.success(`已复制节点 ${c.index} 的接入命令`);
                        }}
                      >
                        复制
                      </Button>
                    </div>
                  ))}
                </div>
                <div className="mt-1 text-[11px] leading-relaxed text-[#86909c]">
                  脚本会在设备上安装本地 SOCKS5 与常驻隧道；首次运行会打印公钥，
                  需加入服务器 <code>authorized_keys</code>（或由管理员执行授权命令）。
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </Card>
  );
}
