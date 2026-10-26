import { useEffect, useState } from "react";
import type { User } from "@/lib/types";

/** undefined = 还在请求中，null = 未登录 */
export type SessionState = User | null | undefined;

let cached: SessionState;
let pending: Promise<SessionState> | null = null;

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

/** 登录/退出后清缓存 */
export function invalidateSession() {
  cached = undefined;
  pending = null;
}

export function useSession(): SessionState {
  const [user, setUser] = useState<SessionState>(cached);

  useEffect(() => {
    if (cached !== undefined) {
      setUser(cached);
      return;
    }
    let alive = true;
    fetchSession().then((u) => {
      if (alive) setUser(u);
    });
    return () => {
      alive = false;
    };
  }, []);

  return user;
}
