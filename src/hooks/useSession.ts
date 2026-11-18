import { useEffect, useState } from "react";
import type { User } from "@/lib/types";

/** undefined = 还在请求中，null = 未登录 */
export type SessionState = User | null | undefined;

let cached: SessionState;
let pending: Promise<SessionState> | null = null;
const listeners = new Set<() => void>();

/** 拉取当前登录用户（进程内缓存，避免每次切页都请求） */
export function fetchSession(): Promise<SessionState> {
  if (cached !== undefined) return Promise.resolve(cached);
  if (!pending) {
    pending = fetch("/api/auth/me")
      .then((r) => r.json())
      .then((j) => (j.ok ? (j.user as User) : null))
      .catch(() => null)
      .then((u) => {
        cached = u;
        pending = null;
        return u;
      });
  }
  return pending;
}

/** 登录/退出/充值/下单/核销后清缓存并通知所有挂载组件刷新余额与会话态 */
export function invalidateSession() {
  cached = undefined;
  pending = null;
  listeners.forEach((fn) => fn());
}

export function useSession(): SessionState {
  const [user, setUser] = useState<SessionState>(cached);

  useEffect(() => {
    let alive = true;
    const sync = () => {
      if (cached !== undefined) {
        if (alive) setUser(cached);
        return;
      }
      fetchSession().then((u) => {
        if (alive) setUser(u);
      });
    };
    listeners.add(sync);
    sync();
    return () => {
      alive = false;
      listeners.delete(sync);
    };
  }, []);

  return user;
}
